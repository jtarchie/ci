import { defineConfig } from 'vitepress';

export default defineConfig({
  title: 'CI',
  description: 'Local-first CI runtime documentation',
  base: '/docs/',
  cleanUrls: false,
  themeConfig: {
    nav: [
      { text: 'Guides', link: '/run' },
      { text: 'Operations', link: '/secrets' },
      { text: 'Drivers', link: '/driver-dsn' },
    ],
    sidebar: [
      {
        text: 'Guides',
        items: [
          { text: 'Run Pipelines', link: '/run' },
          { text: 'MCP', link: '/mcp' },
          { text: 'Webhooks', link: '/webhooks' },
        ],
      },
      {
        text: 'Operations',
        items: [
          { text: 'Secrets', link: '/secrets' },
          { text: 'Caching', link: '/caching' },
          { text: 'Feature Gates', link: '/feature-gates' },
        ],
      },
      {
        text: 'Drivers',
        items: [
          { text: 'Driver DSNs', link: '/driver-dsn' },
          { text: 'Native Resources', link: '/native-resources' },
          { text: 'Implementing Drivers', link: '/implementing-new-driver' },
        ],
      },
    ],
    socialLinks: [
      { icon: 'github', link: 'https://github.com/jtarchie/ci' },
    ],
  },
});
