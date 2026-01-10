# Server Static Assets

This directory contains the frontend dependencies and build configuration for
the CI server's web interface.

## Overview

The static assets are bundled for offline mode using:

- **Tailwind CSS 4.0+** - Utility-first CSS framework
- **Asciinema Player** - Terminal recording player
- **esbuild** - JavaScript bundler

## Building

To build the bundled assets:

```bash
cd server/static
npm install
npm run build
```

Or from the project root:

```bash
task build:static
```

The build process generates two files in `dist/`:

- `bundle.css` - All CSS including Tailwind and Asciinema Player styles
- `bundle.js` - All JavaScript including Asciinema Player

These files are embedded into the Go binary via `//go:embed` and served at
`/static/`.

## Development

The static assets are automatically rebuilt when running:

```bash
task default  # Full build including static assets
task server   # Development server with live reload
```

Note: the development `server` task watches source files under
`server/static/src`. It intentionally does NOT watch the generated
`server/static/dist` folder or `server/static/node_modules` to avoid triggering
rebuilds from the generated bundles (which would create a watch/rebuild loop).

## Dependencies

- **asciinema-player** (^3.9.0) - Terminal session player
- **tailwindcss** (^4.0.0) - CSS framework
- **@tailwindcss/cli** (^4.0.0) - Tailwind CLI for building
- **esbuild** (^0.25.11) - JavaScript bundler

## Files

- `package.json` - Node.js dependencies and scripts
- `build.js` - Build orchestration script
- `src/index.js` - JavaScript entry point
- `src/index.css` - CSS entry point with Tailwind directives
- `dist/` - Generated bundle files (embedded in Go binary)

## Offline Mode

All assets are bundled and embedded into the Go binary, so the server works
completely offline without any external CDN dependencies.
