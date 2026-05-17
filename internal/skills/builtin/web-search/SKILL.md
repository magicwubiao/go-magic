---
name: web-search
description: "Search the web for information using DuckDuckGo"
version: 1.0.0
author: go-magic
tags: [search, web, information, research]
tools: [web_search, web_extract]
---

# Web Search Skill

## When to Use

Load this skill when:
- The user asks about current events, news, or recent information
- You need to find facts, statistics, or data
- The user asks about a specific topic and you need to verify information
- Researching topics that require up-to-date information

## How It Works

### Step 1: Use web_search
Search for relevant results using the search query.

### Step 2: Use web_extract
Extract detailed content from promising URLs.

### Step 3: Synthesize
Combine and summarize the findings for the user.

## Best Practices

1. **Be Specific**: Use specific search terms for better results
2. **Verify Sources**: Cross-reference important information
3. **Respect Robots**: Check robots.txt for crawl permissions
4. **Summarize**: Don't just dump raw results; synthesize

## Search Tips

- Use quotes for exact phrase matching: `"exact phrase"`
- Use site: for specific websites: `site:github.com`
- Use - to exclude terms: `python -snake`
- Combine terms: `AI AND machine learning`

## Example

```
web_search: { query: "latest AI developments 2024" }
web_extract: { url: "https://example.com/article" }
```

## Related Tools

- `web_search`: Search DuckDuckGo for results
- `web_extract`: Extract content from URLs
