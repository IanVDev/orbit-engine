import { describe, test, expect } from "vitest";
import {
  classifyExecutionEvent,
  computeHealthLevel,
  buildDiagnostics,
} from "../observatory-aggregator";

// ── Classificação de evento individual ───────────────────────────────────────

describe("classifyExecutionEvent", () => {
  test("evento válido → trusted", () => {
    const result = classifyExecutionEvent({
      version: 1,
      timestamp: "2026-01-01T00:00:00Z",
      proof: "abc123def456abc123def456abc123def456abc123def456abc123def456ab12",
    });
    expect(result.trust).toBe("trusted");
    expect(result.reason).toBeUndefined();
  });

  test("sem schema_version → rejected", () => {
    const result = classifyExecutionEvent({
      timestamp: "2026-01-01T00:00:00Z",
      command: "go test ./...",
    });
    expect(result.trust).toBe("rejected");
    expect(result.reason).toBe("schema_version_missing");
  });

  test("timestamp inválido → rejected", () => {
    const result = classifyExecutionEvent({
      version: 1,
      timestamp: "nao-é-uma-data",
      proof: "abc123",
    });
    expect(result.trust).toBe("rejected");
    expect(result.reason).toBe("timestamp_invalid");
  });

  test("timestamp ausente → rejected", () => {
    const result = classifyExecutionEvent({ version: 1, proof: "abc123" });
    expect(result.trust).toBe("rejected");
    expect(result.reason).toBe("timestamp_invalid");
  });

  test("sem proof_hash → degraded", () => {
    const result = classifyExecutionEvent({
      version: 1,
      timestamp: "2026-01-01T00:00:00Z",
    });
    expect(result.trust).toBe("degraded");
    expect(result.reason).toBe("proof_hash_missing");
  });
});

// ── Nível de saúde geral ─────────────────────────────────────────────────────

describe("computeHealthLevel", () => {
  test("todos trusted, sem execution_without_log → ok", () => {
    expect(
      computeHealthLevel({ trusted: 5, degraded: 0, rejected: 0, executionWithoutLog: 0 }),
    ).toBe("ok");
  });

  test("execution_without_log > 0 → degraded", () => {
    expect(
      computeHealthLevel({ trusted: 1, degraded: 0, rejected: 0, executionWithoutLog: 1 }),
    ).toBe("degraded");
  });

  test("eventos rejected → degraded", () => {
    expect(
      computeHealthLevel({ trusted: 1, degraded: 0, rejected: 1, executionWithoutLog: 0 }),
    ).toBe("degraded");
  });

  test("execution_without_log > 0 E rejected > 0 → degraded", () => {
    expect(
      computeHealthLevel({ trusted: 1, degraded: 0, rejected: 1, executionWithoutLog: 2 }),
    ).toBe("degraded");
  });

  test("apenas degraded → degraded, não critical", () => {
    expect(
      computeHealthLevel({ trusted: 3, degraded: 2, rejected: 0, executionWithoutLog: 0 }),
    ).toBe("degraded");
  });
});

// ── Anti-regressão: contrato fail-closed completo ────────────────────────────
//
// Dado:
//   1 evento válido
//   1 evento inválido sem schema_version
//   execution_without_log_total = 1
//
// Então:
//   - 1 evento trusted
//   - 1 evento rejected
//   - health = DEGRADED
//   - diagnostics[] contém EVENTS_REJECTED e EXECUTION_WITHOUT_LOG

describe("anti-regressão — contrato fail-closed do aggregator", () => {
  test("1 válido + 1 sem schema_version + execution_without_log=1 → degraded + diagnostics", () => {
    const events: Record<string, unknown>[] = [
      {
        version: 1,
        timestamp: "2026-04-25T10:00:00Z",
        proof: "deadbeef".repeat(8),
        command: "go test ./...",
        exit_code: 0,
      },
      {
        timestamp: "2026-04-25T10:01:00Z",
        command: "make build",
        exit_code: 1,
        // sem "version" → deve ser rejected
      },
    ];

    const classifications = events.map(classifyExecutionEvent);

    const trusted = classifications.filter((c) => c.trust === "trusted").length;
    const rejected = classifications.filter((c) => c.trust === "rejected").length;
    const degraded = classifications.filter((c) => c.trust === "degraded").length;

    expect(trusted).toBe(1);
    expect(rejected).toBe(1);
    expect(degraded).toBe(0);

    const rejectedReasons: Record<string, number> = {};
    for (const c of classifications) {
      if (c.trust === "rejected" && c.reason) {
        rejectedReasons[c.reason] = (rejectedReasons[c.reason] ?? 0) + 1;
      }
    }

    const health = computeHealthLevel({
      trusted,
      degraded,
      rejected,
      executionWithoutLog: 1,
    });
    expect(health).toBe("degraded");

    const diagnostics = buildDiagnostics({
      rejected,
      rejectedReasons,
      degradedCount: degraded,
      executionWithoutLog: 1,
    });

    expect(diagnostics.length).toBeGreaterThan(0);
    expect(diagnostics.some((d) => d.code === "EVENTS_REJECTED")).toBe(true);
    expect(diagnostics.some((d) => d.code === "EXECUTION_WITHOUT_LOG")).toBe(true);

    const rejectedDiag = diagnostics.find((d) => d.code === "EVENTS_REJECTED")!;
    expect(rejectedDiag.count).toBe(1);
    expect(rejectedDiag.message).toContain("schema_version_missing");

    const withoutLogDiag = diagnostics.find((d) => d.code === "EXECUTION_WITHOUT_LOG")!;
    expect(withoutLogDiag.count).toBe(1);
  });
});
