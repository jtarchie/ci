const { execSync } = require("child_process");
const fs = require("fs");
const path = require("path");

async function build() {
  try {
    // Create dist directory if it doesn't exist
    if (!fs.existsSync("dist")) {
      fs.mkdirSync("dist", { recursive: true });
    }

    console.log("🔨 Building CSS with Tailwind...");
    execSync("npm run build:css", { stdio: "inherit" });

    console.log("🔨 Building JavaScript with esbuild...");
    execSync("npm run build:js", { stdio: "inherit" });

    console.log("✅ Build completed successfully");
  } catch (error) {
    console.error("❌ Build failed:", error);
    process.exit(1);
  }
}

build();
