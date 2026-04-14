#!/usr/bin/env python3
"""
orbit-engine feedback collector.

Parses exported Claude Code session logs, detects orbit-engine activations,
computes adoption metrics per activation, and writes a JSONL output file.

Usage:
    python3 tests/feedback_collector.py sessions/session_001.txt
    python3 tests/feedback_collector.py sessions/               # all .txt files
    python3 tests/feedback_collector.py sessions/ --out fb.jsonl

Dependencies: Python stdlib only (json, re, pathlib, dataclasses).
"""

from __future__ import annotations

import json
import re
import sys
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from pathlib import Path


# ---------------------------------------------------------------------------
# Data model
# ---------------------------------------------------------------------------

@dataclass
class Activation:
    """One orbit-engine activation with computed feedback metrics."""

    session_id: str
    activation_id: str
    timestamp: str
    risk: str | None = None
    pattern: str | None = None
    diagnosis_items: int = 0
    action_items: int = 0
    actions_adopted: int = 0
    time_to_action: int | None = None
    clarification_requests: int = 0
    silence: bool = False
    post_action_rework: int = 0
    pattern_recurrence: bool = False
    validation_score: float | None = None


@dataclass
class Turn:
    """A single conversational turn."""

    role: str  # "user" or "assistant"
    text: str
    index: int  # 0-based position in conversation


# ---------------------------------------------------------------------------
# Session parser — converts raw text into turns
# ---------------------------------------------------------------------------

# Heuristics for turn boundaries.  Claude Code exports vary, so we try
# several patterns and fall back to double-newline splitting.

_ROLE_RE = re.compile(
    r"^(?:Human|User|You|H):\s*",
    re.IGNORECASE | re.MULTILINE,
)

_ASSISTANT_RE = re.compile(
    r"^(?:Assistant|Claude|A|orbit-engine):\s*",
    re.IGNORECASE | re.MULTILINE,
)

_TURN_SEPARATOR_RE = re.compile(
    r"\n---+\n|\n={3,}\n|\n\*{3,}\n",
)


def parse_session(text: str) -> list[Turn]:
    """Parse raw session text into a list of Turn objects."""
    turns: list[Turn] = []

    # Strategy 1: explicit role prefixes (Human: / Assistant:)
    role_splits = re.split(
        r"^((?:Human|User|You|H|Assistant|Claude|A|orbit-engine):)",
        text,
        flags=re.IGNORECASE | re.MULTILINE,
    )

    if len(role_splits) > 2:
        # role_splits = ['preamble', 'Human:', 'content', 'Assistant:', 'content', ...]
        idx = 0
        i = 1
        while i < len(role_splits) - 1:
            marker = role_splits[i].strip().rstrip(":").lower()
            content = role_splits[i + 1].strip()
            role = "user" if marker in ("human", "user", "you", "h") else "assistant"
            if content:
                turns.append(Turn(role=role, text=content, index=idx))
                idx += 1
            i += 2
        if turns:
            return turns

    # Strategy 2: separator-based (--- or ===)
    parts = _TURN_SEPARATOR_RE.split(text)
    if len(parts) >= 3:
        for i, part in enumerate(parts):
            part = part.strip()
            if not part:
                continue
            # Alternate user/assistant starting with user
            role = "user" if i % 2 == 0 else "assistant"
            turns.append(Turn(role=role, text=part, index=len(turns)))
        if turns:
            return turns

    # Strategy 3: double-newline paragraphs
    paragraphs = re.split(r"\n\n+", text.strip())
    for i, para in enumerate(paragraphs):
        para = para.strip()
        if not para:
            continue
        role = "user" if i % 2 == 0 else "assistant"
        turns.append(Turn(role=role, text=para, index=len(turns)))

    return turns


# ---------------------------------------------------------------------------
# Activation detector — finds orbit-engine outputs in assistant turns
# ---------------------------------------------------------------------------

_DIAGNOSIS_RE = re.compile(r"^DIAGNOSIS\s*$", re.MULTILINE)
_RISK_RE = re.compile(r"Risk:\s*(low|medium|high|critical)", re.IGNORECASE)
_BULLET_RE = re.compile(r"^\s*[-•]\s+(.+)", re.MULTILINE)
_NUMBERED_RE = re.compile(r"^\s*(?:⚠️\s*)?\d+\.\s+(.+)", re.MULTILINE)
_ACTIONS_RE = re.compile(r"^ACTIONS\s*$", re.MULTILINE)


def find_activations(turns: list[Turn]) -> list[tuple[int, dict]]:
    """Find all assistant turns containing an orbit-engine activation.

    Returns (turn_index, info_dict) pairs.
    """
    results = []
    for turn in turns:
        if turn.role != "assistant":
            continue
        if not _DIAGNOSIS_RE.search(turn.text):
            continue

        info: dict = {}

        # Risk
        risk_m = _RISK_RE.search(turn.text)
        info["risk"] = risk_m.group(1).lower() if risk_m else None

        # Pattern — first diagnosis bullet, simplified
        diag_section = re.search(
            r"DIAGNOSIS\s*\n(.*?)(?=\n\s*(?:Risk:|ACTIONS|DO NOT DO NOW)|$)",
            turn.text, re.DOTALL | re.IGNORECASE,
        )
        if diag_section:
            bullets = _BULLET_RE.findall(diag_section.group(1))
            info["diagnosis_items"] = len(bullets)
            info["pattern"] = _classify_pattern(bullets[0]) if bullets else None
        else:
            info["diagnosis_items"] = 0
            info["pattern"] = None

        # Actions
        actions_section = re.search(
            r"ACTIONS\s*\n(.*?)(?=\n\s*(?:DO NOT DO NOW)|$)",
            turn.text, re.DOTALL | re.IGNORECASE,
        )
        if actions_section:
            items = _NUMBERED_RE.findall(actions_section.group(1))
            info["action_items"] = len(items)
            info["action_texts"] = [t.strip() for t in items]
        else:
            info["action_items"] = 0
            info["action_texts"] = []

        results.append((turn.index, info))

    return results


# ---------------------------------------------------------------------------
# Pattern classifier — maps first diagnosis bullet to a pattern name
# ---------------------------------------------------------------------------

_PATTERN_KEYWORDS: dict[str, list[str]] = {
    "correction_chain": ["correction", "follow-up", "correcting", "rework"],
    "repeated_edits": ["edited", "re-edit", "same file", "rework pattern"],
    "weak_prompt": ["no constraint", "no scope", "missing constraint", "weak prompt",
                     "speculation", "no planning", "without constraint"],
    "unsolicited_long": ["exceeded", "long response", "over-generated", "far exceeded"],
    "exploratory_reading": ["files read", "aimless", "exploratory", "without.*plan"],
    "large_paste": ["pasted", "large block", "large content", "inlined"],
    "ambiguous_intent": ["ambiguous", "unclear", "multiple interpretation", "could mean"],
    "explicit_request": ["explicit", "analyze cost", "efficiency"],
}


def _classify_pattern(bullet: str) -> str:
    """Classify a diagnosis bullet into a named pattern."""
    lower = bullet.lower()
    for pattern_name, keywords in _PATTERN_KEYWORDS.items():
        for kw in keywords:
            if re.search(kw, lower):
                return pattern_name
    return "unknown"


# ---------------------------------------------------------------------------
# Adoption analyzer — computes metrics from turns after an activation
# ---------------------------------------------------------------------------

# Command adoption signals
_CMD_SIGNALS: dict[str, re.Pattern] = {
    "/clear": re.compile(r"/clear\b", re.IGNORECASE),
    "/compact": re.compile(r"/compact\b", re.IGNORECASE),
    "plan_mode": re.compile(r"plan\s*mode|shift\+tab", re.IGNORECASE),
    "@file": re.compile(r"@\w+", re.IGNORECASE),
}

# Prompt improvement signals
_PROMPT_SIGNALS: dict[str, re.Pattern] = {
    "constraints": re.compile(
        r"\b(only|don'?t touch|just|scope|boundary|limit)\b", re.IGNORECASE
    ),
    "define_done": re.compile(
        r"(done when|finished means|success\s*=|acceptance criteria)", re.IGNORECASE
    ),
    "subtasks": re.compile(
        r"(step \d|first.*then|1\.|subtask)", re.IGNORECASE
    ),
}

# Confusion signals
_CONFUSION_RE = re.compile(
    r"(what do you mean|what does that mean|i don'?t understand|why\?|huh\?|"
    r"can you explain|not sure what|unclear)",
    re.IGNORECASE,
)

# Ignore signals (acknowledgment without action)
_IGNORE_RE = re.compile(
    r"^(ok|okay|thanks|thank you|got it|sure|cool|nice|👍)\s*[.!]?\s*$",
    re.IGNORECASE,
)


def compute_adoption(
    turns: list[Turn],
    activation_index: int,
    info: dict,
    next_activation_index: int | None = None,
) -> dict:
    """Compute adoption metrics for a single activation.

    Looks at user turns between activation_index and next_activation_index
    (or end of session).
    """
    window_end = next_activation_index if next_activation_index else len(turns)
    # Limit analysis window to 5 turns after activation
    window_end = min(window_end, activation_index + 6)

    user_turns_after = [
        t for t in turns
        if t.role == "user"
        and activation_index < t.index < window_end
    ]

    action_texts: list[str] = info.get("action_texts", [])
    adopted = set()
    time_to_action: int | None = None
    clarifications = 0

    for i, ut in enumerate(user_turns_after):
        text = ut.text

        # Check confusion
        if _CONFUSION_RE.search(text):
            clarifications += 1
            continue

        # Check command signals
        for cmd_name, pattern in _CMD_SIGNALS.items():
            if pattern.search(text):
                for j, action in enumerate(action_texts):
                    if cmd_name.lstrip("/") in action.lower() or cmd_name in action.lower():
                        adopted.add(j)
                        if time_to_action is None:
                            time_to_action = i

        # Check prompt improvement signals
        for sig_name, pattern in _PROMPT_SIGNALS.items():
            if pattern.search(text):
                for j, action in enumerate(action_texts):
                    action_lower = action.lower()
                    if any(kw in action_lower for kw in [
                        "restate", "constraint", "boundary", "scope",
                        "done", "criteria", "break", "subtask", "step",
                    ]):
                        adopted.add(j)
                        if time_to_action is None:
                            time_to_action = i

        # Check keyword echo — user mentions significant terms from actions
        user_words = set(re.findall(r"[a-zA-Z_]\w{4,}", text.lower()))
        if user_words:
            for j, action in enumerate(action_texts):
                if j in adopted:
                    continue
                action_words = set(re.findall(r"[a-zA-Z_]\w{4,}", action.lower()))
                # Filter out common filler words
                filler = {"would", "could", "should", "about", "their", "there",
                          "these", "those", "which", "where", "before", "after",
                          "using", "first", "instead"}
                action_sig = action_words - filler
                overlap = user_words & action_sig
                # If 2+ significant words from the action appear in user text
                if len(overlap) >= 2:
                    adopted.add(j)
                    if time_to_action is None:
                        time_to_action = i

    # Silence detection: all turns are ignore signals or empty
    silence = all(
        _IGNORE_RE.match(ut.text.strip()) or not ut.text.strip()
        for ut in user_turns_after
    ) if user_turns_after else True

    # Post-action rework: count repeated file edits in window
    rework = _count_rework(turns, activation_index, window_end)

    # Pattern recurrence: same pattern fires again later
    recurrence = False  # computed at session level by caller

    return {
        "actions_adopted": len(adopted),
        "time_to_action": time_to_action,
        "clarification_requests": clarifications,
        "silence": silence and len(adopted) == 0,
        "post_action_rework": rework,
    }


def _count_rework(turns: list[Turn], start: int, end: int) -> int:
    """Count file references that appear 2+ times in assistant turns within window."""
    file_re = re.compile(r"[\w/\\]+\.\w{1,4}", re.IGNORECASE)
    file_mentions: dict[str, int] = {}

    for turn in turns:
        if turn.role != "assistant" or not (start < turn.index < end):
            continue
        for match in file_re.findall(turn.text):
            name = match.lower().split("/")[-1]
            file_mentions[name] = file_mentions.get(name, 0) + 1

    return sum(1 for count in file_mentions.values() if count >= 2)


# ---------------------------------------------------------------------------
# Session processor — ties it all together
# ---------------------------------------------------------------------------

def process_session(
    text: str,
    session_id: str,
    validation_scores: dict[str, float] | None = None,
) -> list[Activation]:
    """Process a full session text into a list of Activation records."""
    turns = parse_session(text)
    activations_found = find_activations(turns)

    results: list[Activation] = []
    patterns_seen: list[str] = []

    for seq, (turn_idx, info) in enumerate(activations_found):
        aid = f"a_{seq + 1:03d}"
        now = datetime.now(timezone.utc).isoformat(timespec="seconds")

        # Next activation boundary
        next_idx = (
            activations_found[seq + 1][0]
            if seq + 1 < len(activations_found)
            else None
        )

        adoption = compute_adoption(turns, turn_idx, info, next_idx)

        # Pattern recurrence
        current_pattern = info.get("pattern", "unknown")
        recurrence = current_pattern in patterns_seen
        patterns_seen.append(current_pattern)

        # Validation score lookup
        vscore = None
        if validation_scores and current_pattern in validation_scores:
            vscore = validation_scores[current_pattern]

        act = Activation(
            session_id=session_id,
            activation_id=aid,
            timestamp=now,
            risk=info.get("risk"),
            pattern=current_pattern,
            diagnosis_items=info.get("diagnosis_items", 0),
            action_items=info.get("action_items", 0),
            actions_adopted=adoption["actions_adopted"],
            time_to_action=adoption["time_to_action"],
            clarification_requests=adoption["clarification_requests"],
            silence=adoption["silence"],
            post_action_rework=adoption["post_action_rework"],
            pattern_recurrence=recurrence,
            validation_score=vscore,
        )
        results.append(act)

    return results


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def main() -> int:
    args = sys.argv[1:]
    if not args or "--help" in args or "-h" in args:
        print("Usage: python3 tests/feedback_collector.py <path> [--out file.jsonl]")
        print()
        print("  <path>   A .txt session file, or a directory of .txt files.")
        print("  --out    Output JSONL path (default: feedback.jsonl)")
        return 0

    # Parse args
    out_path = Path("feedback.jsonl")
    source_path = Path(args[0])
    if "--out" in args:
        idx = args.index("--out")
        if idx + 1 < len(args):
            out_path = Path(args[idx + 1])

    # Collect input files
    if source_path.is_dir():
        files = sorted(source_path.glob("*.txt"))
    elif source_path.is_file():
        files = [source_path]
    else:
        print(f"Error: {source_path} not found.", file=sys.stderr)
        return 1

    if not files:
        print(f"No .txt files found in {source_path}", file=sys.stderr)
        return 1

    # Process
    all_activations: list[Activation] = []
    for f in files:
        session_id = f.stem
        text = f.read_text(encoding="utf-8")
        activations = process_session(text, session_id)
        all_activations.extend(activations)
        print(f"  {f.name}: {len(activations)} activation(s)")

    # Write JSONL
    with open(out_path, "w", encoding="utf-8") as fh:
        for act in all_activations:
            fh.write(json.dumps(asdict(act), ensure_ascii=False) + "\n")

    print()
    print(f"Wrote {len(all_activations)} entries to {out_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
