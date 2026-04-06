import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

const sidebars: SidebarsConfig = {
  docs: [
    "index",
    "getting-started",
    {
      type: "category",
      label: "Concepts",
      items: [
        "concepts/agent-vaults",
        "concepts/guardrails",
        "concepts/spending-limits",
        "concepts/acquired-balance-model",
      ],
    },
    {
      type: "category",
      label: "SDK Reference",
      items: ["sdk/client", "sdk/types", "sdk/chain-config"],
    },
    {
      type: "category",
      label: "Framework Guides",
      items: ["frameworks/langchain", "frameworks/eliza", "frameworks/goat"],
    },
    {
      type: "category",
      label: "Smart Contracts",
      items: [
        "contracts/module",
        "contracts/vault-factory",
        "contracts/preset-registry",
        "contracts/module-registry",
        "contracts/parsers",
      ],
    },
    "security",
    "faq",
  ],
};

export default sidebars;
