import { readdir, readFile } from "fs/promises";
import { existsSync } from "fs";
import path from "path";
import os from "os";

const ORBIT_DIR = path.join(os.homedir(), ".orbit");
const LOGS_DIR = path.join(ORBIT_DIR, "logs");
const LEDGER_PATH = path.join(ORBIT_DIR, "client_ledger.jsonl");

interface ExecLog {
  timestamp?: string;
  command?: string;
  language?: string;
  exit_code?: number;
  duration_ms?: number;
}

interface LedgerEntry {
  impact_estimated_tokens?: number;
}

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
}

async function parseLogs(): Promise<ExecLog[]> {
  if (!existsSync(LOGS_DIR)) return [];

  const files = await readdir(LOGS_DIR);
  const jsonFiles = files.filter((f) => f.endsWith(".json")).sort();

  const logs: ExecLog[] = [];
  for (const file of jsonFiles) {
    const content = await readFile(path.join(LOGS_DIR, file), "utf-8");
    const data = JSON.parse(content);
    if (typeof data !== "object" || data === null || Array.isArray(data)) {
      throw new Error(`Evento inválido em ${file}: esperado objeto`);
    }
    logs.push(data as ExecLog);
  }
  return logs;
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

function aggregate(logs: ExecLog[], ledger: LedgerEntry[]): DashboardStats {
  const total = logs.length;

  if (total === 0) {
    return {
      total_execucoes: 0,
      sucesso: 0,
      falhas: 0,
      taxa_verificacao_pct: 0,
      tempo_medio_ms: 0,
      comandos: {},
      linguagens: {},
      skill_events: ledger.length,
      tokens_estimados: 0,
      ultimo_evento: null,
      atualizado_em: new Date().toISOString(),
    };
  }

  const sucesso = logs.filter((e) => e.exit_code === 0).length;
  const falhas = total - sucesso;

  const durations = logs
    .map((e) => e.duration_ms)
    .filter((d): d is number => typeof d === "number");
  const tempo_medio = durations.length
    ? durations.reduce((a, b) => a + b, 0) / durations.length
    : 0;

  const comandos: Record<string, number> = {};
  const linguagens: Record<string, number> = {};
  const timestamps: string[] = [];

  for (const e of logs) {
    const cmd = e.command ?? "unknown";
    comandos[cmd] = (comandos[cmd] ?? 0) + 1;

    const lang = e.language ?? "unknown";
    linguagens[lang] = (linguagens[lang] ?? 0) + 1;

    if (e.timestamp) timestamps.push(e.timestamp);
  }

  const sortedCmds = Object.fromEntries(
    Object.entries(comandos).sort(([, a], [, b]) => b - a),
  );
  const sortedLangs = Object.fromEntries(
    Object.entries(linguagens).sort(([, a], [, b]) => b - a),
  );

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
    comandos: sortedCmds,
    linguagens: sortedLangs,
    skill_events: ledger.length,
    tokens_estimados,
    ultimo_evento: timestamps.length ? [...timestamps].sort().at(-1)! : null,
    atualizado_em: new Date().toISOString(),
  };
}

export async function GET() {
  try {
    const [logs, ledger] = await Promise.all([parseLogs(), parseLedger()]);
    const stats = aggregate(logs, ledger);
    return Response.json(stats);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    return Response.json(
      { error: message, fail_closed: true },
      { status: 500 },
    );
  }
}

export const dynamic = "force-dynamic";
