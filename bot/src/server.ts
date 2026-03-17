import express from "express";
import cors from "cors";
import { chatHandler } from "./chat";
import { statsHandler } from "./stats";

const PORT = parseInt(process.env.PORT || "3001");
const CORS_ORIGIN = process.env.CORS_ORIGIN || "http://localhost:5173";

const app = express();
app.use(cors({ origin: CORS_ORIGIN }));
app.use(express.json());

// Chat endpoint — receives user messages, returns agent responses
app.post("/api/chat", chatHandler);

// Stats endpoint — returns vault balance and attempt count
app.get("/api/stats", statsHandler);

// Health check
app.get("/api/health", (_req, res) => {
  res.json({ status: "ok", uptime: process.uptime() });
});

app.listen(PORT, () => {
  console.log(`Break the Vault bot running on port ${PORT}`);
  console.log(`CORS: ${CORS_ORIGIN}`);
});
