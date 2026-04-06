import { themes as prismThemes } from "prism-react-renderer";
import type { Config } from "@docusaurus/types";
import type * as Preset from "@docusaurus/preset-classic";

const config: Config = {
  title: "MultiClaw",
  tagline: "On-chain guardrails for AI agents",
  favicon: "img/favicon.ico",

  future: {
    v4: true,
  },

  url: "https://docs.multiclaws.xyz",
  baseUrl: "/",

  organizationName: "xaviermiel",
  projectName: "MultiClaw",

  onBrokenLinks: "throw",

  i18n: {
    defaultLocale: "en",
    locales: ["en"],
  },

  presets: [
    [
      "classic",
      {
        docs: {
          sidebarPath: "./sidebars.ts",
          routeBasePath: "/",
          editUrl:
            "https://github.com/xaviermiel/MultiClaw/tree/main/docs-site/",
        },
        blog: false,
        theme: {
          customCss: "./src/css/custom.css",
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: "MultiClaw",
      items: [
        {
          type: "docSidebar",
          sidebarId: "docs",
          position: "left",
          label: "Docs",
        },
        {
          href: "https://app.multiclaws.xyz",
          label: "App",
          position: "right",
        },
        {
          href: "https://github.com/xaviermiel/MultiClaw",
          label: "GitHub",
          position: "right",
        },
      ],
    },
    footer: {
      style: "dark",
      links: [
        {
          title: "Documentation",
          items: [
            { label: "Getting Started", to: "/getting-started" },
            { label: "SDK Reference", to: "/sdk/client" },
            { label: "Contracts", to: "/contracts/module" },
          ],
        },
        {
          title: "Resources",
          items: [
            {
              label: "GitHub",
              href: "https://github.com/xaviermiel/MultiClaw",
            },
            {
              label: "npm",
              href: "https://www.npmjs.com/package/@multiclaw/core",
            },
          ],
        },
      ],
      copyright: `Copyright ${new Date().getFullYear()} DUSA LABS. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ["solidity", "bash", "json"],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
