#!/bin/bash
# Build WASM plugins using TinyGo
# Usage:
#   ./build-plugins.sh              # Build all plugins
#   ./build-plugins.sh welcome-bot  # Build single plugin

set -e

PLUGINS_DIR="plugins"
TINYGO_TARGET="wasi"

build_plugin() {
    local dir="$1"
    local name=$(basename "$dir")
    
    if [ ! -f "$dir/main.go" ]; then
        echo "  ⏭ $name — no main.go, skipping"
        return
    fi
    
    if [ ! -f "$dir/manifest.json" ]; then
        echo "  ⏭ $name — no manifest.json, skipping"
        return
    fi
    
    echo "  🔨 Building $name..."
    tinygo build -o "$dir/plugin.wasm" -target "$TINYGO_TARGET" -no-debug "./$dir/"
    
    local size=$(ls -lh "$dir/plugin.wasm" | awk '{print $5}')
    echo "  ✅ $name → plugin.wasm ($size)"
}

echo "╔══════════════════════════════════════╗"
echo "║   Saybridge WASM Plugin Builder      ║"
echo "╚══════════════════════════════════════╝"
echo ""

if [ -n "$1" ]; then
    # Build specific plugin
    target="$PLUGINS_DIR/$1"
    if [ ! -d "$target" ]; then
        echo "❌ Plugin '$1' not found in $PLUGINS_DIR/"
        exit 1
    fi
    build_plugin "$target"
else
    # Build all plugins
    echo "Building all plugins in $PLUGINS_DIR/..."
    echo ""
    
    count=0
    for dir in "$PLUGINS_DIR"/*/; do
        if [ -d "$dir" ]; then
            build_plugin "$dir"
            count=$((count + 1))
        fi
    done
    
    echo ""
    echo "Done! Processed $count plugins."
fi
