#!/bin/bash
echo "Testing Jekyll configuration..."
echo "1. Checking _config.yml"
cat _config.yml
echo ""
echo "2. Checking Gemfile"
cat Gemfile
echo ""
echo "3. Trying to simulate Jekyll build structure"
echo "If this were built by Jekyll, we'd have:"
echo "- _site/index.html"
echo "- _site/*.html from *.md files"
echo ""
echo "4. Manual test: Check if basic structure is valid"
if [ -f "_config.yml" ] && [ -f "index.md" ]; then
  echo "✓ Basic files exist"
else
  echo "✗ Missing basic files"
fi
