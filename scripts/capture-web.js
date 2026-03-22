#!/usr/bin/env node

/**
 * Standalone web screenshot capture script for The Running Man
 * 
 * This script captures screenshots of web applications using Playwright.
 * It's designed to be used by AI agents for debugging web application layout issues.
 * 
 * Usage:
 *   node capture-web.js --url <url> [--selector <selector>] [--output <path>] [--viewport <size>] [--timeout <ms>]
 * 
 * Examples:
 *   node capture-web.js --url http://localhost:3000 --output screenshot.png
 *   node capture-web.js --url http://localhost:3000 --selector ".app-container" --output element.png
 *   node capture-web.js --url http://localhost:3000 --viewport 1920x1080 --output desktop.png
 */

const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');
const { program } = require('commander');

// Parse command line arguments
program
  .name('capture-web')
  .description('Capture screenshots of web applications for debugging')
  .requiredOption('-u, --url <url>', 'URL to capture (required)')
  .option('-s, --selector <selector>', 'CSS selector for element-specific screenshot')
  .option('-o, --output <path>', 'Output file path (default: screenshot-{timestamp}.png)')
  .option('-v, --viewport <size>', 'Viewport size (widthxheight, default: 1280x720)', '1280x720')
  .option('-t, --timeout <ms>', 'Page load timeout in milliseconds (default: 10000)', '10000')
  .option('--full-page', 'Capture full page (scrolls to capture entire page)', false)
  .option('--wait-for <selector>', 'Wait for selector to appear before capturing')
  .option('--wait-time <ms>', 'Wait time after page load in milliseconds (default: 1000)', '1000')
  .option('--debug', 'Enable debug logging', false)
  .parse(process.argv);

const options = program.opts();

// Validate URL
function isValidUrl(string) {
  try {
    new URL(string);
    return true;
  } catch (_) {
    return false;
  }
}

if (!isValidUrl(options.url)) {
  console.error(`Error: Invalid URL format: ${options.url}`);
  console.error('Please provide a valid URL including protocol (http:// or https://)');
  process.exit(1);
}

// Parse viewport
const [width, height] = options.viewport.split('x').map(Number);
if (isNaN(width) || isNaN(height) || width <= 0 || height <= 0) {
  console.error(`Error: Invalid viewport size: ${options.viewport}`);
  console.error('Please use format: widthxheight (e.g., 1920x1080)');
  process.exit(1);
}

// Generate output filename if not provided
let outputPath = options.output;
if (!outputPath) {
  const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
  const hostname = new URL(options.url).hostname.replace(/[^a-zA-Z0-9]/g, '-');
  outputPath = `screenshot-${hostname}-${timestamp}.png`;
}

// Ensure output directory exists
const outputDir = path.dirname(outputPath);
if (outputDir && !fs.existsSync(outputDir)) {
  fs.mkdirSync(outputDir, { recursive: true });
}

async function captureScreenshot() {
  let browser = null;
  let page = null;
  
  try {
    if (options.debug) {
      console.log(`Starting screenshot capture for: ${options.url}`);
      console.log(`Viewport: ${width}x${height}`);
      console.log(`Output: ${outputPath}`);
    }
    
    // Launch browser
    browser = await chromium.launch({
      headless: true,
      args: ['--no-sandbox', '--disable-setuid-sandbox']
    });
    
    // Create new page
    const context = await browser.newContext({
      viewport: { width, height },
      userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
    });
    
    page = await context.newPage();
    
    // Set timeout
    page.setDefaultTimeout(parseInt(options.timeout));
    
    // Navigate to URL
    if (options.debug) console.log(`Navigating to ${options.url}...`);
    await page.goto(options.url, { waitUntil: 'networkidle' });
    
    // Wait for additional time if specified
    if (parseInt(options.waitTime) > 0) {
      if (options.debug) console.log(`Waiting ${options.waitTime}ms after page load...`);
      await page.waitForTimeout(parseInt(options.waitTime));
    }
    
    // Wait for specific selector if requested
    if (options.waitFor) {
      if (options.debug) console.log(`Waiting for selector: ${options.waitFor}...`);
      await page.waitForSelector(options.waitFor, { timeout: parseInt(options.timeout) });
    }
    
    // Capture screenshot
    if (options.debug) console.log('Capturing screenshot...');
    
    if (options.selector) {
      // Element-specific screenshot
      const element = await page.$(options.selector);
      if (!element) {
        throw new Error(`Element not found with selector: ${options.selector}`);
      }
      await element.screenshot({ path: outputPath });
    } else {
      // Full page or viewport screenshot
      await page.screenshot({
        path: outputPath,
        fullPage: options.fullPage
      });
    }
    
    if (options.debug) console.log(`Screenshot saved to: ${outputPath}`);
    console.log(`Success: Screenshot saved to ${outputPath}`);
    
  } catch (error) {
    console.error(`Error capturing screenshot: ${error.message}`);
    
    // Provide helpful error messages for common issues
    if (error.message.includes('net::ERR_CONNECTION_REFUSED')) {
      console.error('\nConnection refused. Possible issues:');
      console.error('1. The web server is not running');
      console.error('2. The URL port is incorrect');
      console.error('3. The server is not accessible from this network');
    } else if (error.message.includes('net::ERR_NAME_NOT_RESOLVED')) {
      console.error('\nDNS resolution failed. Possible issues:');
      console.error('1. The hostname is incorrect');
      console.error('2. DNS is not configured properly');
      console.error('3. The URL format is invalid');
    } else if (error.message.includes('timeout')) {
      console.error('\nPage load timeout. Possible issues:');
      console.error('1. The server is slow to respond');
      console.error('2. Increase timeout with --timeout <ms>');
      console.error('3. Network connectivity issues');
    } else if (error.message.includes('playwright')) {
      console.error('\nPlaywright error. Possible issues:');
      console.error('1. Playwright browsers not installed');
      console.error('2. Run: npx playwright install chromium');
    } else if (error.message.includes('net::ERR_EMPTY_RESPONSE') || error.message.includes('net::ERR_CONNECTION_RESET')) {
      console.error('\nEmpty response or connection reset. Possible issues:');
      console.error('1. The server is not responding');
      console.error('2. The port might be open but no service is running');
      console.error('3. Firewall or network issues');
    }
    
    process.exit(1);
  } finally {
    // Cleanup
    if (page) await page.close();
    if (browser) await browser.close();
  }
}

// Check if Playwright is available
try {
  require.resolve('playwright');
} catch (error) {
  console.error('Error: Playwright is not installed.');
  console.error('\nTo install Playwright:');
  console.error('1. Install Playwright: npm install playwright');
  console.error('2. Install browsers: npx playwright install chromium');
  console.error('\nOr install globally:');
  console.error('  npm install -g playwright');
  console.error('  playwright install chromium');
  process.exit(1);
}

// Run the capture
captureScreenshot();