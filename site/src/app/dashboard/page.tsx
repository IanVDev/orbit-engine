"use client";

import { useEffect, useState, useCallback } from "react";

interface DashboardStats {
  total_execucoes: number;
  sucesso: number;
  falhas: number;
  taxa_verificacao_pct: number;
  tempo_medio_ms: number;
  comandos: Record<string, number>;
  linguagens: Record<string, number>;
  skill_events: number;
  tokens_estimados: number;
  ultimo_evento: string | null;
  atualizado_em: string;
  error?: string;
  fail_closed?: boolean;
}

const POLL_INTERVAL_MS = 5000;

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

function CommandBar({
  name,
  count,
  total,
}: {
  name: string;
  count: number;
  total: number;
}) {
  const pct = total > 0 ? (count / total) * 100 : 0;
  return (
    <div className="flex items-center gap-3">
      <span className="font-mono text-sm text-text-2 w-20 shrink-0">{name}</span>
      <div className="flex-1 h-2 bg-surface-2 rounded-full overflow-hidden">
        <div
          className="h-full bg-accent rounded-full transition-all duration-300"
          style={{ width: `${pct}%` }}
        />
      </div>
      <span className="font-mono text-sm text-text-3 w-8 text-right">
        {count}
      </span>
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
          Carregando dados do Orbit...
        </span>
      </div>
    );
  }

  if (fetchError) {
    return (
      <div className="min-h-screen flex items-center justify-center p-8">
        <div className="rounded-[var(--radius-lg)] border border-atrisk/40 bg-surface/70 p-6 max-w-lg w-full">
          <div className="font-mono text-xs text-atrisk uppercase tracking-wider mb-2">
            ORBIT — FAIL CLOSED
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

  return (
    <div className="max-w-[var(--container-site)] mx-auto px-4 py-10 sm:py-16">
      {/* Header */}
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-2xl font-bold text-text tracking-tight">
            Orbit — Dashboard
          </h1>
          <p className="text-sm text-text-3 font-mono mt-1">
            Dados locais de <code className="text-text-2">~/.orbit/logs/</code>
          </p>
        </div>
        <div className="text-right">
          <div className="flex items-center gap-2 justify-end">
            <span className="w-2 h-2 rounded-full bg-healthy animate-pulse inline-block" />
            <span className="text-xs font-mono text-text-3">ao vivo</span>
          </div>
          {lastPoll && (
            <div className="text-xs font-mono text-text-3 mt-1">
              {fmtTime(lastPoll.toISOString())}
            </div>
          )}
        </div>
      </div>

      {/* Métricas principais */}
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3 mb-6">
        <StatCard
          label="Execuções"
          value={stats.total_execucoes}
          accent="accent"
        />
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
          accent="accent"
        />
        <StatCard
          label="Skill Events"
          value={stats.skill_events}
          sub={`~${stats.tokens_estimados.toLocaleString("pt-BR")} tokens`}
          accent="accent"
        />
      </div>

      {/* Comandos + Linguagens */}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-6">
        {/* Comandos mais usados */}
        <div className="rounded-[var(--radius-lg)] border border-border bg-surface/70 p-5">
          <h2 className="text-xs font-mono text-text-3 uppercase tracking-wider mb-4">
            Comandos mais usados
          </h2>
          <div className="flex flex-col gap-3">
            {topCmds.map(([name, count]) => (
              <CommandBar
                key={name}
                name={name}
                count={count}
                total={cmdTotal}
              />
            ))}
          </div>
        </div>

        {/* Linguagens */}
        <div className="rounded-[var(--radius-lg)] border border-border bg-surface/70 p-5">
          <h2 className="text-xs font-mono text-text-3 uppercase tracking-wider mb-4">
            Linguagens
          </h2>
          <div className="flex flex-col gap-3">
            {Object.entries(stats.linguagens)
              .slice(0, 5)
              .map(([name, count]) => (
                <CommandBar
                  key={name}
                  name={name}
                  count={count}
                  total={stats.total_execucoes}
                />
              ))}
          </div>
        </div>
      </div>

      {/* Último evento */}
      <div className="rounded-[var(--radius-lg)] border border-border-soft bg-surface/40 p-4 flex flex-col sm:flex-row sm:items-center gap-2 sm:gap-6">
        <div>
          <span className="text-xs font-mono text-text-3 uppercase tracking-wider">
            Último evento registrado
          </span>
          <div className="font-mono text-sm text-text mt-1">
            {fmtTime(stats.ultimo_evento)}
          </div>
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
