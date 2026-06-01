#!/usr/bin/env node
/**
 * Web Scraper Plugin for go-magic
 * Fetches and extracts content from web pages
 */

const https = require('https');
const http = require('http');
const { URL } = require('url');

// Parse arguments
const args = process.argv.slice(2);
let url = null;
let mode = 'text'; // text, links, images, all

for (const arg of args) {
  if (arg.startsWith('--url=')) {
    url = arg.replace('--url=', '');
  } else if (arg.startsWith('--mode=')) {
    mode = arg.replace('--mode=', '');
  }
}

if (!url) {
  console.log(JSON.stringify({
    error: 'URL is required. Usage: node run.js --url=https://example.com [--mode=text|links|images|all]'
  }, null, 2));
  process.exit(1);
}

function fetchUrl(targetUrl) {
  return new Promise((resolve, reject) => {
    const parsedUrl = new URL(targetUrl);
    const protocol = parsedUrl.protocol === 'https:' ? https : http;
    
    const options = {
      hostname: parsedUrl.hostname,
      port: parsedUrl.port,
      path: parsedUrl.pathname + parsedUrl.search,
      method: 'GET',
      headers: {
        'User-Agent': 'go-magic-plugin/1.0 (Web Scraper)'
      }
    };

    const req = protocol.request(options, (res) => {
      let data = '';
      
      res.on('data', (chunk) => {
        data += chunk;
      });
      
      res.on('end', () => {
        resolve({
          status: res.statusCode,
          headers: res.headers,
          body: data
        });
      });
    });

    req.on('error', (err) => {
      reject(err);
    });

    req.setTimeout(30000, () => {
      req.destroy();
      reject(new Error('Request timeout'));
    });

    req.end();
  });
}

function extractText(html) {
  // Remove script and style tags
  let text = html.replace(/<script\b[^<]*(?:(?!<\/script>)<[^<]*)*<\/script>/gi, '');
  text = text.replace(/<style\b[^<]*(?:(?!<\/style>)<[^<]*)*<\/style>/gi, '');
  
  // Remove HTML tags
  text = text.replace(/<[^>]+>/g, ' ');
  
  // Clean up whitespace
  text = text.replace(/\s+/g, ' ').trim();
  
  return text;
}

function extractLinks(html) {
  const links = [];
  const linkRegex = /<a[^>]+href=["']([^"']+)["'][^>]*>([^<]*)<\/a>/gi;
  let match;
  
  while ((match = linkRegex.exec(html)) !== null) {
    const href = match[1];
    const text = match[2].trim();
    
    // Skip empty and anchor links
    if (href && !href.startsWith('#') && href !== '#') {
      links.push({
        url: href,
        text: text || href
      });
    }
  }
  
  return links.slice(0, 50); // Limit to 50 links
}

function extractImages(html) {
  const images = [];
  const imgRegex = /<img[^>]+src=["']([^"']+)["'][^>]*>/gi;
  let match;
  
  while ((match = imgRegex.exec(html)) !== null) {
    const imgTag = match[0];
    const src = match[1];
    
    let alt = '';
    let title = '';
    
    const altMatch = imgTag.match(/alt=["']([^"']*)["']/i);
    const titleMatch = imgTag.match(/title=["']([^"']*)["']/i);
    
    if (altMatch) alt = altMatch[1];
    if (titleMatch) title = titleMatch[1];
    
    if (src && !src.startsWith('data:')) {
      images.push({
        url: src,
        alt: alt,
        title: title
      });
    }
  }
  
  return images.slice(0, 20); // Limit to 20 images
}

async function main() {
  try {
    const result = await fetchUrl(url);
    
    const response = {
      url: url,
      status: result.status,
      content_type: result.headers['content-type'] || 'text/html'
    };

    // Check if content is HTML
    if (!response.content_type.includes('text/html')) {
      response.data = result.body.substring(0, 10000);
      console.log(JSON.stringify(response, null, 2));
      return;
    }

    switch (mode) {
      case 'text':
        response.text = extractText(result.body).substring(0, 5000);
        break;
        
      case 'links':
        response.links = extractLinks(result.body);
        break;
        
      case 'images':
        response.images = extractImages(result.body);
        break;
        
      case 'all':
        response.text = extractText(result.body).substring(0, 3000);
        response.links = extractLinks(result.body);
        response.images = extractImages(result.body);
        break;
        
      default:
        response.text = extractText(result.body).substring(0, 5000);
    }

    console.log(JSON.stringify(response, null, 2));
    
  } catch (error) {
    console.log(JSON.stringify({
      error: error.message,
      url: url
    }, null, 2));
    process.exit(1);
  }
}

main();
