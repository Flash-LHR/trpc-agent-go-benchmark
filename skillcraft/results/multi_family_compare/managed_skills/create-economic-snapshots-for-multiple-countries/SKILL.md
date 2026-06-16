---
name: Create Economic Snapshots for Multiple Countries
description: Create economic snapshots for specified countries by collecting data through multiple API endpoints including economic overviews, GDP data, and country information, then compiling the results into a structured JSON report.
---

# Create Economic Snapshots for Multiple Countries

## When to use

When you need to analyze and summarize economic data for three or more countries using various APIs.

## Steps

1. Define the list of countries to analyze.
2. Use the economic snapshots API to gather a comprehensive economic overview for each country.
3. Fetch GDP data for each country using the appropriate API.
4. Collect basic country information using the country info API.
5. Calculate economic power rank and development tier for each country.
6. Compile all data into a single JSON report and save it to the specified location.

## Pitfalls

- Ensure the country codes are correct to avoid data retrieval errors.
- Verify that all required indicators are included in the final report.
