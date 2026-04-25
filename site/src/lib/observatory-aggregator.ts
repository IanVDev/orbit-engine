// observatory-aggregator.ts — funções puras de classificação e saúde.
// Sem dependência de FS ou Node.js — testável em isolamento total.
// Contrato fail-closed: qualquer desvio de schema → rejected ou degraded.

export type TrustLevel = "ok" | "degraded" | "critical";
export type EventTrust = "trusted" | "degraded" | "rejected";

export interface ObservatoryDiagnostic {
  code: string;
  message: string;
  count?: number;
}

export interface EventClassification {
  trust: EventTrust;
  reason?: string;
}

// classifyExecutionEvent valida um evento de execução contra o contrato
// mínimo do Observatory. Não lança exceção — retorna classificação.
//
// Contratos:
//   Sem "version" (schema_version)  → rejected
//   Sem timestamp ou inválido       → rejected
//   Sem "proof" (proof_hash)        → degraded
//   Tudo presente                   → trusted
export function classifyExecutionEvent(
  data: Record<string, unknown>,
): EventClassification {
  if (!("version" in data)) {
    return { trust: "rejected", reason: "schema_version_missing" };
  }

  const ts = data["timestamp"];
  if (!ts || typeof ts !== "string" || isNaN(new Date(ts).getTime())) {
    return { trust: "rejected", reason: "timestamp_invalid" };
  }

  if (!data["proof"] || typeof data["proof"] !== "string") {
    return { trust: "degraded", reason: "proof_hash_missing" };
  }

  return { trust: "trusted" };
}

// computeHealthLevel deriva o nível de saúde geral do Observatory.
// Fail-closed: qualquer evento rejected, degraded ou execution_without_log > 0
// → mínimo DEGRADED. CRITICAL reservado para "log inválido" (não ocorre aqui).
export function computeHealthLevel(params: {
  trusted: number;
  degraded: number;
  rejected: number;
  executionWithoutLog: number;
}): TrustLevel {
  const { degraded, rejected, executionWithoutLog } = params;

  if (executionWithoutLog > 0 || rejected > 0 || degraded > 0) return "degraded";
  return "ok";
}

// buildDiagnostics gera mensagens legíveis para cada causa de degradação.
export function buildDiagnostics(params: {
  rejected: number;
  rejectedReasons: Record<string, number>;
  degradedCount: number;
  executionWithoutLog: number;
}): ObservatoryDiagnostic[] {
  const { rejected, rejectedReasons, degradedCount, executionWithoutLog } = params;
  const out: ObservatoryDiagnostic[] = [];

  if (rejected > 0) {
    const reasons = Object.entries(rejectedReasons)
      .map(([r, n]) => `${r}: ${n}`)
      .join(", ");
    out.push({
      code: "EVENTS_REJECTED",
      message: `${rejected} evento(s) rejeitado(s) — validação de schema falhou. Motivos: ${reasons || "unknown"}`,
      count: rejected,
    });
  }

  if (degradedCount > 0) {
    out.push({
      code: "EVENTS_DEGRADED",
      message: `${degradedCount} evento(s) sem proof_hash — integridade não verificável`,
      count: degradedCount,
    });
  }

  if (executionWithoutLog > 0) {
    out.push({
      code: "EXECUTION_WITHOUT_LOG",
      message: `${executionWithoutLog} execução(ões) sem log verificado — possível perda de dado`,
      count: executionWithoutLog,
    });
  }

  return out;
}
