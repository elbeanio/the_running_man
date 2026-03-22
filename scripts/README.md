# Screenshot Capture Scripts

This directory contains standalone scripts for capturing screenshots of web applications, designed to help AI agents debug web application layout issues.

## capture-web.js

A Node.js script using Playwright to capture screenshots of web applications.

### Installation

```bash
# Install Node.js dependencies
npm install playwright commander

# Install Playwright browsers
npx playwright install chromium
```

### Usage

```bash
node capture-web.js --url <url> [options]
```

### Options

| Option | Description | Default |
|--------|-------------|---------|
| `-u, --url <url>` | URL to capture (required) | - |
| `-s, --selector <selector>` | CSS selector for element-specific screenshot | - |
| `-o, --output <path>` | Output file path | `screenshot-{hostname}-{timestamp}.png` |
| `-v, --viewport <size>` | Viewport size (widthxheight) | `1280x720` |
| `-t, --timeout <ms>` | Page load timeout in milliseconds | `10000` |
| `--full-page` | Capture full page (scrolls to capture entire page) | `false` |
| `--wait-for <selector>` | Wait for selector to appear before capturing | - |
| `--wait-time <ms>` | Wait time after page load in milliseconds | `1000` |
| `--debug` | Enable debug logging | `false` |

### Examples

```bash
# Basic screenshot of a local web app
node capture-web.js --url http://localhost:3000 --output app.png

# Capture specific element
node capture-web.js --url http://localhost:3000 --selector ".app-container" --output element.png

# Desktop-sized screenshot
node capture-web.js --url http://localhost:3000 --viewport 1920x1080 --output desktop.png

# Full page screenshot
node capture-web.js --url http://localhost:3000 --full-page --output full-page.png

# Wait for specific element to load
node capture-web.js --url http://localhost:3000 --wait-for "#app-loaded" --output loaded.png
```

### Error Handling

The script provides helpful error messages for common issues:

- **Connection refused**: Web server not running, incorrect port, or network issues
- **DNS resolution failed**: Invalid hostname or DNS issues
- **Timeout**: Server slow to respond (increase timeout with `--timeout`)
- **Playwright not installed**: Install Playwright and browsers

### Integration with The Running Man

This script is designed to be called by:
1. **AI agents directly** for debugging web applications
2. **The Running Man MCP tools** (future integration)
3. **Manual debugging** when visual context is needed

### Security Considerations

- Only captures screenshots of accessible URLs
- Runs in headless mode with sandbox disabled for compatibility
- No authentication or sensitive data handling
- Local file system access only for output

### Development

To modify the script:

```bash
# Make the script executable
chmod +x capture-web.js

# Test with a local server
python3 -m http.server 8000 &
node capture-web.js --url http://localhost:8000 --debug
```

### Dependencies

- Node.js 14+
- Playwright 1.40+
- Commander.js 11+

### Future Enhancements

Planned features for future versions:
1. Multiple screenshot formats (JPEG, WebP)
2. PDF export
3. Video recording
4. Performance metrics capture
5. Integration with The Running Man API