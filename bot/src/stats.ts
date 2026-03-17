import type { Request, Response } from "express";
import { getVaultStats } from "./agent";
import { getAttemptCount } from "./chat";

export async function statsHandler(_req: Request, res: Response) {
  try {
    const vaultStats = await getVaultStats();

    res.json({
      balance: vaultStats.balance,
      totalAttempts: getAttemptCount(),
      isPaused: vaultStats.isPaused,
      lastUpdated: new Date().toISOString(),
    });
  } catch (error: unknown) {
    console.error("Stats error:", error);
    res.json({
      balance: "Unknown",
      totalAttempts: getAttemptCount(),
      isPaused: false,
      lastUpdated: new Date().toISOString(),
    });
  }
}
