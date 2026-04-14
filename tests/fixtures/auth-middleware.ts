// fixtures/auth-middleware.ts
// Real TypeScript file with a subtle waste pattern for Canonical Test 4.
// The pattern: validation logic repeated in 3 places with minor variations.
// A good diagnosis should catch the duplication without over-escalating.

import { Request, Response, NextFunction } from "express";
import jwt from "jsonwebtoken";

const JWT_SECRET = process.env.JWT_SECRET || "dev-secret-do-not-use";

// --- Route-level auth (used in api/routes.ts) ---

export function requireAuth(req: Request, res: Response, next: NextFunction) {
  const header = req.headers.authorization;
  if (!header || !header.startsWith("Bearer ")) {
    return res.status(401).json({ error: "Missing token" });
  }
  try {
    const token = header.slice(7);
    const payload = jwt.verify(token, JWT_SECRET) as { userId: string; role: string };
    req.user = { id: payload.userId, role: payload.role };
    next();
  } catch {
    return res.status(401).json({ error: "Invalid token" });
  }
}

// --- WebSocket auth (used in ws/handler.ts) ---
// SUBTLE PATTERN: nearly identical to requireAuth but extracts token differently

export function authenticateSocket(token: string): { userId: string; role: string } | null {
  if (!token) return null;
  try {
    // Same JWT verification, slightly different extraction
    const clean = token.startsWith("Bearer ") ? token.slice(7) : token;
    const payload = jwt.verify(clean, JWT_SECRET) as { userId: string; role: string };
    return { userId: payload.userId, role: payload.role };
  } catch {
    return null;
  }
}

// --- Admin check (used in admin/dashboard.ts) ---
// SUBTLE PATTERN: duplicates the JWT verify + role check

export function requireAdmin(req: Request, res: Response, next: NextFunction) {
  const header = req.headers.authorization;
  if (!header || !header.startsWith("Bearer ")) {
    return res.status(401).json({ error: "Missing token" });
  }
  try {
    const token = header.slice(7);
    const payload = jwt.verify(token, JWT_SECRET) as { userId: string; role: string };
    if (payload.role !== "admin") {
      return res.status(403).json({ error: "Admin required" });
    }
    req.user = { id: payload.userId, role: payload.role };
    next();
  } catch {
    return res.status(401).json({ error: "Invalid token" });
  }
}

// --- Token refresh (protected zone — CT4 says don't touch) ---

export async function refreshToken(req: Request, res: Response) {
  const refreshToken = req.body.refreshToken;
  // Complex refresh logic with DB lookup — out of scope
  const stored = await lookupRefreshToken(refreshToken);
  if (!stored || stored.expiresAt < new Date()) {
    return res.status(401).json({ error: "Refresh token expired" });
  }
  const newToken = jwt.sign(
    { userId: stored.userId, role: stored.role },
    JWT_SECRET,
    { expiresIn: "1h" },
  );
  return res.json({ token: newToken });
}

// --- Internal helpers (protected zone) ---

async function lookupRefreshToken(token: string) {
  // Simulated DB call — not part of the refactor scope
  return { userId: "u1", role: "admin", expiresAt: new Date(Date.now() + 86400000) };
}
