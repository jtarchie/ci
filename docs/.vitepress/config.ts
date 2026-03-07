import { defineConfig } from "vitepress";

export default defineConfig({
  title: "PocketCI",
  description: "Local-first PocketCI runtime documentation",
  base: "/docs/",
  cleanUrls: false,
  themeConfig: {
    nav: [
      { text: "Guides", link: "/guides/" },
      { text: "Runtime API", link: "/runtime/" },
      { text: "Operations", link: "/operations/" },
      { text: "Drivers", link: "/drivers/" },
    ],
    sidebar: {
      "/runtime/": [
        { text: "Overview", link: "/runtime/" },
        { text: "runtime.run()", link: "runtime-run" },
        { text: "runtime.agent()", link: "runtime-agent" },
        { text: "Volumes", link: "volumes" },
      ],
      "/guides/": [
        { text: "Overview", link: "/guides/" },
        { text: "Run Pipelines", link: "run" },
        { text: "Webhooks", link: "webhooks" },
        { text: "MCP", link: "mcp" },
      ],
      "/operations/": [
        { text: "Overview", link: "/operations/" },
        { text: "Secrets", link: "secrets" },
        { text: "Caching", link: "caching" },
        { text: "Feature Gates", link: "feature-gates" },
      ],
      "/drivers/": [
        { text: "Overview", link: "/drivers/" },
        { text: "Driver DSNs", link: "dsn" },
        { text: "Native Resources", link: "native-resources" },
        { text: "Implementing Drivers", link: "implementing-driver" },
      ],
    },
    socialLinks: [
      { icon: "github", link: "https://github.com/jtarchie/pocketci" },
    ],
  },
});
