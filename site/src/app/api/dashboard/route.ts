import { readdir, readFile } from "fs/promises";
import { existsSync } from "fs";
import path from "path";
import os from "os";

const ORBIT_DIR = path.join(os.homedir(), ".orbit");
const LOGS_DIR = path.join(ORBIT_DIR, "logs");
const LEDGER_PATH = path.join(ORBIT_DIR, "client_ledger.jsonl");

const FAILURE_TYPES: Record<number, string> = {
  0: "none",
  1: "runtime_error",
  7: "verification_failed",
  127: "command_not_found",
  254: "system_error",
};

// Padrão de nome de arquivo: {ts}_{nano?}_{hex8}_exit{code}.json
const FILENAME_SESSION_RE = /([0-9a-f]{8})_exit\d+\.json$/;

// Campos obrigatórios em logs versionados (version >= 1)
const EXECUTION_ESSENTIAL = ["timestamp", "exit_code", "command", "language"] as const;

// Contrato do payload `diagnosis` persistido pelo Go no momento do run
// (ver tracking/cmd/orbit/diagnose.go → DiagnosisPayload).
//
// Este parser é FONTE SECUNDÁRIA: consome apenas o que está no log.
// Nunca infere de `output` — o Go já rodou o parser.
//
// Fail-closed:
//   - ausente          → ignorado (log antigo)
//   - não-objeto       → ignorado
//   - sem confidence   → ignorado
//   - confidence=none  → ignorado (parser disse "não sei")
type Confidence = "high" | "medium" | "none";

interface DiagnosisPayload {
  version?: number;
  error_type?: string;
  test_name?: string;
  file?: string;
  line?: number;
  message?: string;
  confidence?: Confidence;
}

interface RawLog {
  version?: number;
  timestamp?: string;
  command?: string;
  language?: string;
  event?: string;
  exit_code?: number;
  duration_ms?: number;
  execution_id?: string;
  anchor_status?: string;
  diagnosis?: DiagnosisPayload;
  [key: string]: unknown;
}

interface DiagnosisView {
  timestamp: string;
  command: string;
  event: string;
  exit_code: number;
  error_type: string;
  test_name: string;
  file: string;
  line: number;
  message: string;
  confidence: "high" | "medium";
}

interface ExecLog extends RawLog {
  session_id: string | null;
  parent_event_id: string | null;
  failure_type: string;
}

interface LedgerEntry {
  timestamp?: string;
  impact_estimated_tokens?: number;
  parent_event_id?: string;
  link_method?: "temporal";
  link_confidence?: "low";
  link_semantic?: "non_causal";
  link_window_seconds?: 60;
  [key: string]: unknown;
}

interface DashboardStats {
  total_execucoes: number;
  sucesso: number;
  falhas: number;
  taxa_verificacao_pct: number;
  tempo_medio_ms: number;
  p50_ms: number;
  p95_ms: number;
  failure_types: Record<string, number>;
  comandos: Record<string, number>;
  linguagens: Record<string, number>;
  session_count: number;
  anchor_events: number;
  skill_events: number;
  tokens_estimados: number;
  ultimo_evento: string | null;
  recent_diagnoses: DiagnosisView[];
  atualizado_em: string;
}

const VALID_CONFIDENCE: ReadonlySet<string> = new Set(["high", "medium"]);
const RECENT_DIAGNOSES_LIMIT = 10;

// Fail-closed: devolve view pronta para surfacear ou null.
function extractDiagnosisView(log: ExecLog): DiagnosisView | null {
  const d = log.diagnosis;
  if (!d || typeof d !== "object") return null;
  const c = d.confidence;
  if (c !== "high" && c !== "medium") return null;

  return {
    timestamp: log.timestamp ?? "",
    command: log.command ?? "",
    event: log.event ?? "",
    exit_code: typeof log.exit_code === "number" ? log.exit_code : 0,
    error_type: d.error_type ?? "",
    test_name: d.test_name ?? "",
    file: d.file ?? "",
    line: typeof d.line === "number" ? d.line : 0,
    message: d.message ?? "",
    confidence: c,
  };
}

function collectRecentDiagnoses(executions: ExecLog[]): DiagnosisView[] {
  const views: DiagnosisView[] = [];
  for (const e of executions) {
    const v = extractDiagnosisView(e);
    if (v) views.push(v);
  }
  views.sort((a, b) => (a.timestamp < b.timestamp ? 1 : a.timestamp > b.timestamp ? -1 : 0));
  return views.slice(0, RECENT_DIAGNOSES_LIMIT);
}

function failureType(exitCode: number | undefined): string {
  if (exitCode === undefined) return "unknown";
  return FAILURE_TYPES[exitCode] ?? `exit_${exitCode}`;
}

function deriveSessionId(filename: string): string | null {
  const m = filename.match(FILENAME_SESSION_RE);
  return m ? m[1] : null;
}

function percentile(data: number[], pct: number): number {
  if (!data.length) return 0;
  const s = [...data].sort((a, b) => a - b);
  const idx = (pct / 100) * (s.length - 1);
  const lo = Math.floor(idx);
  const hi = lo + 1;
  if (hi >= s.length) return Math.round(s[s.length - 1] * 10) / 10;
  const frac = idx - lo;
  return Math.round((s[lo] + frac * (s[hi] - s[lo])) * 10) / 10;
}

function isExecutionLog(data: RawLog): boolean {
  return "exit_code" in data || "version" in data;
}

function validateExecution(data: RawLog, file: string): void {
  if (data.version !== undefined) {
    for (const field of EXECUTION_ESSENTIAL) {
      if (!(field in data)) {
        throw new Error(`Campo essencial '${field}' ausente em ${file}`);
      }
    }
  }
}

function tsToEpoch(iso: string): number {
  try {
    return new Date(iso).getTime() / 1000;
  } catch {
    return 0;
  }
}

function linkSkillEvents(executions: ExecLog[], ledger: LedgerEntry[]): void {
  if (!executions.length || !ledger.length) return;

  const execTimes = executions.map((e) => ({
    id: e.execution_id ?? null,
    ts: tsToEpoch(e.timestamp ?? ""),
  }));

  for (const entry of ledger) {
    const entryTs = tsToEpoch(entry.timestamp ?? "");
    if (!entryTs) continue;

    let closestId: string | null = null;
    let closestDelta = Infinity;

    for (const { id, ts } of execTimes) {
      const delta = Math.abs(entryTs - ts);
      if (delta < closestDelta && delta <= 60) {
        closestDelta = delta;
        closestId = id;
      }
    }

    if (closestId) entry.parent_event_id = closestId;
  }
}

async function parseLogs(): Promise<{ executions: ExecLog[]; anchors: RawLog[] }> {
  if (!existsSync(LOGS_DIR)) return { executions: [], anchors: [] };

  const files = (await readdir(LOGS_DIR)).filter((f) => f.endsWith(".json")).sort();
  const executions: ExecLog[] = [];
  const anchors: RawLog[] = [];

  for (const file of files) {
    const content = await readFile(path.join(LOGS_DIR, file), "utf-8");
    const data = JSON.parse(content) as RawLog;

    if (typeof data !== "object" || data === null || Array.isArray(data)) {
      throw new Error(`Evento inválido em ${file}: esperado objeto`);
    }

    if (!data.timestamp) {
      throw new Error(`Campo essencial 'timestamp' ausente em ${file}`);
    }

    if (isExecutionLog(data)) {
      validateExecution(data, file);
      executions.push({
        ...data,
        session_id: deriveSessionId(file),
        parent_event_id: null,
        failure_type: failureType(data.exit_code),
      });
    } else {
      anchors.push(data);
    }
  }

  return { executions, anchors };
}

async function parseLedger(): Promise<LedgerEntry[]> {
  if (!existsSync(LEDGER_PATH)) return [];

  const content = await readFile(LEDGER_PATH, "utf-8");
  const lines = content.split("\n").filter((l) => l.trim());

  return lines.map((line, i) => {
    const data = JSON.parse(line);
    if (typeof data !== "object" || data === null) {
      throw new Error(`Linha ${i + 1} do ledger inválida`);
    }
    return data as LedgerEntry;
  });
}

function aggregate(
  executions: ExecLog[],
  anchors: RawLog[],
  ledger: LedgerEntry[],
): DashboardStats {
  const total = executions.length;

  if (total === 0) {
    return {
      total_execucoes: 0,
      sucesso: 0,
      falhas: 0,
      taxa_verificacao_pct: 0,
      tempo_medio_ms: 0,
      p50_ms: 0,
      p95_ms: 0,
      failure_types: {},
      comandos: {},
      linguagens: {},
      session_count: 0,
      anchor_events: anchors.length,
      skill_events: ledger.length,
      tokens_estimados: 0,
      ultimo_evento: null,
      recent_diagnoses: [],
      atualizado_em: new Date().toISOString(),
    };
  }

  const sucesso = executions.filter((e) => e.exit_code === 0).length;
  const falhas = total - sucesso;

  const durations = executions
    .map((e) => e.duration_ms)
    .filter((d): d is number => typeof d === "number");

  const tempo_medio = durations.length
    ? durations.reduce((a, b) => a + b, 0) / durations.length
    : 0;

  const comandos: Record<string, number> = {};
  const linguagens: Record<string, number> = {};
  const failure_types: Record<string, number> = {};
  const sessionIds = new Set<string>();
  const timestamps: string[] = [];

  for (const e of executions) {
    const cmd = e.command ?? "unknown";
    comandos[cmd] = (comandos[cmd] ?? 0) + 1;

    const lang = e.language ?? "unknown";
    linguagens[lang] = (linguagens[lang] ?? 0) + 1;

    const ft = e.failure_type;
    failure_types[ft] = (failure_types[ft] ?? 0) + 1;

    if (e.session_id) sessionIds.add(e.session_id);
    if (e.timestamp) timestamps.push(e.timestamp);
  }

  const sorted = (r: Record<string, number>) =>
    Object.fromEntries(Object.entries(r).sort(([, a], [, b]) => b - a));

  const tokens_estimados = ledger.reduce(
    (acc, e) => acc + (e.impact_estimated_tokens ?? 0),
    0,
  );

  return {
    total_execucoes: total,
    sucesso,
    falhas,
    taxa_verificacao_pct: Math.round((sucesso / total) * 1000) / 10,
    tempo_medio_ms: Math.round(tempo_medio * 10) / 10,
    p50_ms: percentile(durations, 50),
    p95_ms: percentile(durations, 95),
    failure_types: sorted(failure_types),
    comandos: sorted(comandos),
    linguagens: sorted(linguagens),
    session_count: sessionIds.size,
    anchor_events: anchors.length,
    skill_events: ledger.length,
    tokens_estimados,
    ultimo_evento: timestamps.length ? [...timestamps].sort().at(-1)! : null,
    recent_diagnoses: collectRecentDiagnoses(executions),
    atualizado_em: new Date().toISOString(),
  };
}

export async function GET() {
  try {
    const [{ executions, anchors }, ledger] = await Promise.all([
      parseLogs(),
      parseLedger(),
    ]);
    linkSkillEvents(executions, ledger);
    const stats = aggregate(executions, anchors, ledger);
    return Response.json(stats);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    return Response.json({ error: message, fail_closed: true }, { status: 500 });
  }
}

export const dynamic = "force-dynamic";
