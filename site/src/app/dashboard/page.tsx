"use client";

import { useEffect, useState, useCallback } from "react";
import type { TrustLevel, ObservatoryDiagnostic } from "@/lib/observatory-aggregator";

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

interface DashboardStats {
  // Observatory health
  trust_level: TrustLevel;
  trusted_events: number;
  rejected_events: number;
  degraded_events: number;
  execution_without_log: number;
  diagnostics: ObservatoryDiagnostic[];
  // Métricas de execução
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
  recent_diagnoses?: DiagnosisView[];
  silenced_events?: number;
  silenced_by_command?: Record<string, number>;
  expansion_policy?: {
    threshold: number;
    window_days: number;
    min_distinct_days: number;
  };
  expansion_candidates?: {
    command: string;
    silenced_count: number;
    distinct_days: number;
  }[];
  atualizado_em: string;
  error?: string;
  fail_closed?: boolean;
}

const POLL_INTERVAL_MS = 5000;

const FAILURE_LABELS: Record<string, string> = {
  none: "Sucesso",
  verification_failed: "Verif. falhou",
  runtime_error: "Runtime",
  command_not_found: "Cmd não encontrado",
  system_error: "Sistema",
  unknown: "Desconhecido",
};

const FAILURE_COLORS: Record<string, string> = {
  none: "bg-healthy",
  verification_failed: "bg-degraded",
  runtime_error: "bg-atrisk",
  command_not_found: "bg-atrisk",
  system_error: "bg-atrisk",
  unknown: "bg-text-3",
};

// Mapeia error_type → label curto mostrado como badge no bloco
// "Diagnoses recentes". Fonte: campo persistido no log pelo parser Go.
// Qualquer error_type desconhecido cai no fallback "ANALYSIS".
const ERROR_TYPE_LABEL: Record<string, string> = {
  go_test_assertion: "TEST",
  go_build_error:    "BUILD",
  file_line_only:    "TEST?",
};

const ERROR_TYPE_STYLE: Record<string, string> = {
  go_test_assertion: "bg-accent/15 text-accent",
  go_build_error:    "bg-degraded/20 text-degraded",
  file_line_only:    "bg-text-3/15 text-text-2",
};

const TRUST_LEVEL_LABEL: Record<TrustLevel, string> = {
  ok: "OK",
  degraded: "DEGRADED",
  critical: "CRITICAL",
};

const TRUST_LEVEL_COLOR: Record<TrustLevel, string> = {
  ok: "text-healthy",
  degraded: "text-degraded",
  critical: "text-atrisk",
};

const TRUST_LEVEL_BORDER: Record<TrustLevel, string> = {
  ok: "border-healthy/30",
  degraded: "border-degraded/50",
  critical: "border-atrisk/60",
};

function fmtTime(iso: string | null): string {
  if (!iso) return "—";
  try {
    return new Date(iso).toLocaleString("pt-BR", {
      day: "2-digit",
      month: "2-digit",
      year: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  } catch {
    return iso;
  }
}

function fmtDuration(ms: number): string {
  if (ms < 1000) return `${ms.toFixed(0)} ms`;
  return `${(ms / 1000).toFixed(2)} s`;
}

function StatCard({
  label,
  value,
  sub,
  accent,
}: {
  label: string;
  value: string | number;
  sub?: string;
  accent?: "healthy" | "atrisk" | "degraded" | "accent";
}) {
  const colorMap = {
    healthy: "text-healthy",
    atrisk: "text-atrisk",
    degraded: "text-degraded",
    accent: "text-accent",
  };
  const valueColor = accent ? colorMap[accent] : "text-text";

  return (
    <div className="rounded-[var(--radius-lg)] border border-border bg-surface/70 p-5 flex flex-col gap-1">
      <span className="text-xs font-mono text-text-3 uppercase tracking-wider">
        {label}
      </span>
      <span className={`text-3xl font-bold font-mono ${valueColor}`}>
        {value}
      </span>
      {sub && <span className="text-xs text-text-2">{sub}</span>}
    </div>
  );
}

function Bar({
  name,
  count,
  total,
  colorClass = "bg-accent",
}: {
  name: string;
  count: number;
  total: number;
  colorClass?: string;
}) {
  const pct = total > 0 ? (count / total) * 100 : 0;
  return (
    <div className="flex items-center gap-3">
      <span className="font-mono text-sm text-text-2 w-28 shrink-0 truncate">{name}</span>
      <div className="flex-1 h-2 bg-surface-2 rounded-full overflow-hidden">
        <div
          className={`h-full ${colorClass} rounded-full transition-all duration-300`}
          style={{ width: `${pct}%` }}
        />
      </div>
      <span className="font-mono text-sm text-text-3 w-8 text-right">{count}</span>
    </div>
  );
}

// TrustLevelCard — primeiro card, mais importante.
// Se DEGRADED ou CRITICAL, mostra diagnostics inline.
function TrustLevelCard({
  level,
  diagnostics,
  trustedEvents,
  rejectedEvents,
  degradedEvents,
  executionWithoutLog,
}: {
  level: TrustLevel;
  diagnostics: ObservatoryDiagnostic[];
  trustedEvents: number;
  rejectedEvents: number;
  degradedEvents: number;
  executionWithoutLog: number;
}) {
  return (
    <div
      className={`rounded-[var(--radius-lg)] border ${TRUST_LEVEL_BORDER[level]} bg-surface/70 p-5 col-span-2 sm:col-span-3 lg:col-span-2`}
    >
      <div className="flex items-center justify-between mb-3">
        <span className="text-xs font-mono text-text-3 uppercase tracking-wider">
          Trust Level
        </span>
        <span
          className={`font-mono text-xs px-2 py-0.5 rounded font-semibold ${
            level === "ok"
              ? "bg-healthy/15 text-healthy"
              : level === "degraded"
                ? "bg-degraded/20 text-degraded"
                : "bg-atrisk/20 text-atrisk"
          }`}
        >
          {TRUST_LEVEL_LABEL[level]}
        </span>
      </div>

      <div className="flex items-baseline gap-2">
        <span className={`text-3xl font-bold font-mono ${TRUST_LEVEL_COLOR[level]}`}>
          {trustedEvents}
        </span>
        <span className="text-xs font-mono text-text-3">
          trusted
          {rejectedEvents > 0 && (
            <span className="text-atrisk ml-2">· {rejectedEvents} rejected</span>
          )}
          {degradedEvents > 0 && (
            <span className="text-degraded ml-2">· {degradedEvents} degraded</span>
          )}
          {executionWithoutLog > 0 && (
            <span className="text-atrisk ml-2">· {executionWithoutLog} sem log</span>
          )}
        </span>
      </div>

      {diagnostics.length > 0 && (
        <div className="mt-3 flex flex-col gap-1.5 border-t border-border-soft pt-3">
          {diagnostics.map((d) => (
            <div key={d.code} className="font-mono text-xs text-text-2">
              <span
                className={`font-semibold mr-1 ${
                  d.code === "EXECUTION_WITHOUT_LOG" ? "text-atrisk" : "text-degraded"
                }`}
              >
                {d.code}:
              </span>
              {d.message}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export default function DashboardPage() {
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [lastPoll, setLastPoll] = useState<Date | null>(null);

  const fetchStats = useCallback(async () => {
    try {
      const res = await fetch("/api/dashboard", { cache: "no-store" });
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: `HTTP ${res.status}` }));
        setFetchError(body.error ?? `HTTP ${res.status}`);
        return;
      }
      const data: DashboardStats = await res.json();
      if (data.fail_closed) {
        setFetchError(data.error ?? "Erro nos dados — fail-closed ativo");
        return;
      }
      setStats(data);
      setFetchError(null);
      setLastPoll(new Date());
    } catch (err) {
      setFetchError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchStats();
    const id = setInterval(fetchStats, POLL_INTERVAL_MS);
    return () => clearInterval(id);
  }, [fetchStats]);

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <span className="font-mono text-text-3 animate-pulse">
          Carregando dados do Orbit Observatory...
        </span>
      </div>
    );
  }

  if (fetchError) {
    return (
      <div className="min-h-screen flex items-center justify-center p-8">
        <div className="rounded-[var(--radius-lg)] border border-atrisk/40 bg-surface/70 p-6 max-w-lg w-full">
          <div className="font-mono text-xs text-atrisk uppercase tracking-wider mb-2">
            ORBIT OBSERVATORY — FAIL CLOSED
          </div>
          <div className="text-text font-mono text-sm">{fetchError}</div>
          <button
            onClick={fetchStats}
            className="mt-4 text-xs font-mono text-accent underline"
          >
            Tentar novamente
          </button>
        </div>
      </div>
    );
  }

  if (!stats) return null;

  const cmdTotal = Object.values(stats.comandos).reduce((a, b) => a + b, 0);
  const topCmds = Object.entries(stats.comandos).slice(0, 5);
  const failureEntries = Object.entries(stats.failure_types).filter(
    ([k]) => k !== "none",
  );

  return (
    <div className="max-w-[var(--container-site)] mx-auto px-4 py-10 sm:py-16">
      {/* Header */}
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-2xl font-bold text-text tracking-tight">
            Orbit Observatory
          </h1>
          <p className="text-sm text-text-3 font-mono mt-1">
            Dados locais de <code className="text-text-2">~/.orbit/logs/</code>
          </p>
        </div>
        <div className="text-right">
          <div className="flex items-center gap-2 justify-end">
            <span
              className={`w-2 h-2 rounded-full inline-block ${
                stats.trust_level === "ok"
                  ? "bg-healthy animate-pulse"
                  : stats.trust_level === "degraded"
                    ? "bg-degraded animate-pulse"
                    : "bg-atrisk animate-pulse"
              }`}
            />
            <span className="text-xs font-mono text-text-3">ao vivo · 5s</span>
          </div>
          {lastPoll && (
            <div className="text-xs font-mono text-text-3 mt-1">
              {fmtTime(lastPoll.toISOString())}
            </div>
          )}
        </div>
      </div>

      {/* Linha 1: Trust Level + métricas principais */}
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3 mb-6">
        <TrustLevelCard
          level={stats.trust_level}
          diagnostics={stats.diagnostics ?? []}
          trustedEvents={stats.trusted_events}
          rejectedEvents={stats.rejected_events}
          degradedEvents={stats.degraded_events}
          executionWithoutLog={stats.execution_without_log}
        />
        <StatCard label="Execuções" value={stats.total_execucoes} accent="accent" />
        <StatCard
          label="Taxa de Sucesso"
          value={`${stats.taxa_verificacao_pct}%`}
          sub={`${stats.sucesso} de ${stats.total_execucoes}`}
          accent={
            stats.taxa_verificacao_pct >= 80
              ? "healthy"
              : stats.taxa_verificacao_pct >= 60
                ? "degraded"
                : "atrisk"
          }
        />
        <StatCard
          label="Falhas"
          value={stats.falhas}
          sub={`${(100 - stats.taxa_verificacao_pct).toFixed(1)}% do total`}
          accent={stats.falhas > 0 ? "atrisk" : "healthy"}
        />
        <StatCard
          label="Tempo Médio"
          value={fmtDuration(stats.tempo_medio_ms)}
          sub={`p50 ${fmtDuration(stats.p50_ms)} · p95 ${fmtDuration(stats.p95_ms)}`}
          accent="accent"
        />
        <StatCard
          label="Skill Events"
          value={stats.skill_events}
          sub={`~${stats.tokens_estimados.toLocaleString("pt-BR")} tokens`}
          accent="accent"
        />
      </div>

      {/* Tipos de falha + Latência */}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-6">
        {/* Breakdown de falhas */}
        <div className="rounded-[var(--radius-lg)] border border-border bg-surface/70 p-5">
          <h2 className="text-xs font-mono text-text-3 uppercase tracking-wider mb-4">
            Tipos de falha
          </h2>
          {failureEntries.length === 0 ? (
            <span className="font-mono text-sm text-healthy">Sem falhas registradas</span>
          ) : (
            <div className="flex flex-col gap-3">
              {failureEntries.map(([type, count]) => (
                <Bar
                  key={type}
                  name={FAILURE_LABELS[type] ?? type}
                  count={count}
                  total={stats.falhas}
                  colorClass={FAILURE_COLORS[type] ?? "bg-text-3"}
                />
              ))}
            </div>
          )}
        </div>

        {/* Latência percentis */}
        <div className="rounded-[var(--radius-lg)] border border-border bg-surface/70 p-5">
          <h2 className="text-xs font-mono text-text-3 uppercase tracking-wider mb-4">
            Latência de execução
          </h2>
          <div className="flex flex-col gap-4">
            {(
              [
                ["Média", stats.tempo_medio_ms],
                ["p50 (mediana)", stats.p50_ms],
                ["p95", stats.p95_ms],
              ] as [string, number][]
            ).map(([label, ms]) => (
              <div key={label} className="flex items-center justify-between">
                <span className="font-mono text-sm text-text-2">{label}</span>
                <span className="font-mono text-sm text-text font-semibold">
                  {fmtDuration(ms)}
                </span>
              </div>
            ))}
            <div className="flex items-center justify-between border-t border-border-soft pt-3">
              <span className="font-mono text-sm text-text-3">Eventos âncora</span>
              <span className="font-mono text-sm text-text-3">{stats.anchor_events}</span>
            </div>
          </div>
        </div>
      </div>

      {/* Comandos + Linguagens */}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-6">
        <div className="rounded-[var(--radius-lg)] border border-border bg-surface/70 p-5">
          <h2 className="text-xs font-mono text-text-3 uppercase tracking-wider mb-4">
            Comandos mais usados
          </h2>
          <div className="flex flex-col gap-3">
            {topCmds.map(([name, count]) => (
              <Bar key={name} name={name} count={count} total={cmdTotal} />
            ))}
          </div>
        </div>

        <div className="rounded-[var(--radius-lg)] border border-border bg-surface/70 p-5">
          <h2 className="text-xs font-mono text-text-3 uppercase tracking-wider mb-4">
            Linguagens
          </h2>
          <div className="flex flex-col gap-3">
            {Object.entries(stats.linguagens)
              .slice(0, 5)
              .map(([name, count]) => (
                <Bar
                  key={name}
                  name={name}
                  count={count}
                  total={stats.total_execucoes}
                />
              ))}
          </div>
        </div>
      </div>

      {/* Silenced events — sinal para evolução do parser.
          Execuções em que o decision engine pediu análise mas nenhum
          parser casou. Se um comando aparecer aqui recorrentemente, é
          o gatilho honesto para adicionar um novo parser ao dispatcher. */}
      {stats.silenced_events !== undefined && stats.silenced_events > 0 && (
        <div className="rounded-[var(--radius-lg)] border border-border-soft bg-surface/50 p-4 mb-6 flex items-center justify-between gap-4 flex-wrap">
          <div>
            <span className="text-xs font-mono text-text-3 uppercase tracking-wider">
              Silenced
            </span>
            <div className="font-mono text-sm text-text mt-1">
              <span className="text-degraded font-semibold">
                {stats.silenced_events}
              </span>{" "}
              execuções pediram análise sem parser correspondente.
            </div>
          </div>
          {stats.silenced_by_command &&
            Object.keys(stats.silenced_by_command).length > 0 && (
              <div className="flex items-center gap-2 flex-wrap">
                {Object.entries(stats.silenced_by_command).map(([cmd, n]) => {
                  // Bucket por binário: o contrato casa candidates via command.split()[0].
                  const bucket = cmd.split(/\s+/)[0] || cmd;
                  const isCandidate = stats.expansion_candidates?.some(
                    (c) => c.command === bucket,
                  );
                  return (
                    <span
                      key={cmd}
                      className={`font-mono text-[10px] px-1.5 py-0.5 rounded ${
                        isCandidate
                          ? "bg-atrisk/20 text-atrisk font-semibold"
                          : "bg-degraded/15 text-degraded"
                      }`}
                      title={
                        isCandidate
                          ? `candidato a novo parser (threshold ${stats.expansion_policy?.threshold}, ${stats.expansion_policy?.window_days}d)`
                          : undefined
                      }
                    >
                      {cmd} · {n}
                      {isCandidate ? " · candidato" : ""}
                    </span>
                  );
                })}
              </div>
            )}
        </div>
      )}

      {/* Diagnoses recentes — vindos direto do campo `diagnosis` do log.
          Parser Go é a fonte; este bloco é só renderização. */}
      {stats.recent_diagnoses && stats.recent_diagnoses.length > 0 && (
        <div className="rounded-[var(--radius-lg)] border border-border bg-surface/70 p-5 mb-6">
          <h2 className="text-xs font-mono text-text-3 uppercase tracking-wider mb-4">
            Diagnoses recentes
          </h2>
          <div className="flex flex-col gap-3">
            {stats.recent_diagnoses.map((d, i) => (
              <div
                key={`${d.timestamp}-${i}`}
                className="border-l-2 border-atrisk/60 pl-3 flex flex-col gap-1"
              >
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="font-mono text-xs text-text-3">
                    {fmtTime(d.timestamp)}
                  </span>
                  <span className="font-mono text-xs text-text-2">
                    {d.command} · {d.event} · exit {d.exit_code}
                  </span>
                  {d.error_type && (
                    <span
                      className={`font-mono text-[10px] px-1.5 py-0.5 rounded ${
                        ERROR_TYPE_STYLE[d.error_type] ?? "bg-text-3/15 text-text-2"
                      }`}
                      title={d.error_type}
                    >
                      {ERROR_TYPE_LABEL[d.error_type] ?? "ANALYSIS"}
                    </span>
                  )}
                  <span
                    className={`font-mono text-[10px] px-1.5 py-0.5 rounded ${
                      d.confidence === "high"
                        ? "bg-atrisk/20 text-atrisk"
                        : "bg-degraded/20 text-degraded"
                    }`}
                  >
                    {d.confidence}
                  </span>
                </div>
                <div className="font-mono text-sm text-text">
                  {d.test_name && <span className="text-accent">{d.test_name} </span>}
                  {d.file && (
                    <span className="text-text-2">
                      @ {d.file}:{d.line}
                    </span>
                  )}
                </div>
                {d.message && (
                  <div className="font-mono text-xs text-text-2 truncate">
                    {d.message}
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Rodapé de metadados */}
      <div className="rounded-[var(--radius-lg)] border border-border-soft bg-surface/40 p-4 flex flex-col sm:flex-row sm:items-center gap-2 sm:gap-6">
        <div>
          <span className="text-xs font-mono text-text-3 uppercase tracking-wider">
            Último evento
          </span>
          <div className="font-mono text-sm text-text mt-1">
            {fmtTime(stats.ultimo_evento)}
          </div>
        </div>
        <div>
          <span className="text-xs font-mono text-text-3 uppercase tracking-wider">
            Sessions (derivadas)
          </span>
          <div className="font-mono text-sm text-text mt-1">{stats.session_count}</div>
        </div>
        <div className="sm:ml-auto">
          <span className="text-xs font-mono text-text-3 uppercase tracking-wider">
            Atualizado em
          </span>
          <div className="font-mono text-sm text-text mt-1">
            {fmtTime(stats.atualizado_em)}
          </div>
        </div>
      </div>
    </div>
  );
}
