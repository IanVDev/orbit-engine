// fixtures/ingestion.ts
// Real TypeScript file with detectable waste patterns for Canonical Test 2.
// Used to validate that orbit-engine produces surgical, grounded diagnosis.

import { Kafka, Consumer, Producer, EachMessagePayload } from "kafkajs";
import { Pool, PoolClient } from "pg";
import { z } from "zod";

// --- Kafka Consumer (DO NOT TOUCH per CT2 constraint) ---

const kafka = new Kafka({
  clientId: "ingestion-service",
  brokers: ["kafka-1:9092", "kafka-2:9092"],
});

const consumer: Consumer = kafka.consumer({ groupId: "ingestion-group" });
const producer: Producer = kafka.producer();

async function startConsumer(): Promise<void> {
  await consumer.connect();
  await consumer.subscribe({ topic: "raw-events", fromBeginning: false });
  await consumer.run({
    eachMessage: async (payload: EachMessagePayload) => {
      const raw = payload.message.value?.toString();
      if (!raw) return;
      const parsed = JSON.parse(raw);
      const result = await transformAndValidate(parsed);
      if (result.success) {
        await writeToDatabase(result.data);
      } else {
        await writeToDeadLetter(parsed, result.errors);
      }
    },
  });
}

// --- Transformation step (TARGET — this is what CT2 should diagnose) ---

// WASTE PATTERN: God function — does validation, transformation, enrichment,
// normalization, and formatting all in one 90-line function.
async function transformAndValidate(raw: any): Promise<any> {
  // Step 1: Schema validation (mixed with transformation)
  const schema = z.object({
    event_type: z.string(),
    timestamp: z.string(),
    payload: z.object({
      user_id: z.string(),
      action: z.string(),
      metadata: z.record(z.unknown()).optional(),
    }),
  });

  let validated: any;
  try {
    validated = schema.parse(raw);
  } catch (e: any) {
    return { success: false, errors: e.errors };
  }

  // Step 2: Normalize timestamp (inline, not extracted)
  let normalizedTimestamp: Date;
  if (validated.timestamp.includes("T")) {
    normalizedTimestamp = new Date(validated.timestamp);
  } else if (validated.timestamp.match(/^\d+$/)) {
    normalizedTimestamp = new Date(parseInt(validated.timestamp));
  } else {
    try {
      normalizedTimestamp = new Date(validated.timestamp);
    } catch {
      normalizedTimestamp = new Date();
    }
  }

  // Step 3: Enrich with derived fields (inline, repeated logic)
  const enriched: any = {
    ...validated,
    processed_at: new Date().toISOString(),
    normalized_timestamp: normalizedTimestamp.toISOString(),
    day_of_week: normalizedTimestamp.getDay(),
    hour_of_day: normalizedTimestamp.getHours(),
    is_weekend: normalizedTimestamp.getDay() === 0 || normalizedTimestamp.getDay() === 6,
  };

  // Step 4: Event type mapping (hardcoded, not configurable)
  const typeMap: Record<string, string> = {
    click: "interaction",
    view: "impression",
    purchase: "conversion",
    signup: "acquisition",
    logout: "session_end",
    error: "system",
  };
  enriched.category = typeMap[enriched.event_type] || "unknown";

  // Step 5: Format for database (yet another inline transformation)
  const dbRecord = {
    id: `${enriched.payload.user_id}-${Date.now()}`,
    event_type: enriched.event_type,
    category: enriched.category,
    user_id: enriched.payload.user_id,
    action: enriched.payload.action,
    metadata: JSON.stringify(enriched.payload.metadata || {}),
    raw_timestamp: enriched.timestamp,
    normalized_timestamp: enriched.normalized_timestamp,
    processed_at: enriched.processed_at,
    day_of_week: enriched.day_of_week,
    hour_of_day: enriched.hour_of_day,
    is_weekend: enriched.is_weekend,
  };

  return { success: true, data: dbRecord };
}

// --- Database Writer (DO NOT TOUCH per CT2 constraint) ---

const pool = new Pool({
  host: "db-primary",
  port: 5432,
  database: "events",
  user: "writer",
  password: process.env.DB_PASSWORD,
});

async function writeToDatabase(record: any): Promise<void> {
  const client: PoolClient = await pool.connect();
  try {
    await client.query(
      `INSERT INTO events (id, event_type, category, user_id, action, metadata,
        raw_timestamp, normalized_timestamp, processed_at, day_of_week, hour_of_day, is_weekend)
       VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
      [
        record.id, record.event_type, record.category, record.user_id,
        record.action, record.metadata, record.raw_timestamp,
        record.normalized_timestamp, record.processed_at, record.day_of_week,
        record.hour_of_day, record.is_weekend,
      ]
    );
  } finally {
    client.release();
  }
}

async function writeToDeadLetter(original: any, errors: any[]): Promise<void> {
  await producer.connect();
  await producer.send({
    topic: "dead-letter",
    messages: [{ value: JSON.stringify({ original, errors, failed_at: new Date().toISOString() }) }],
  });
}

// --- Start ---
startConsumer().catch(console.error);
