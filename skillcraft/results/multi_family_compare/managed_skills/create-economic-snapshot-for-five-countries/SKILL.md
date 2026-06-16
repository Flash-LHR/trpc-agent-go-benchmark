---
name: Create Economic Snapshot for Five Countries
description: Create economic snapshots for five specified major economies using five API endpoints per country, collecting economic overviews, GDP data, country information, economic indicators, and population data while calculating economic power rank and development tier.
---

# Create Economic Snapshot for Five Countries

## When to use

Use when needing to generate a detailed economic report for five countries using multiple data sources for comprehensive economic insights.

## Steps

1. Specify the five countries to analyze.
2. Use the `mcp_local-worldbank_economic_snapshot` API to gather economic snapshots for each country.
3. Use the `mcp_local-worldbank_gdp` API to gather GDP data for each country.
4. Use the `mcp_local-worldbank_country_info` API to retrieve basic country information.
5. Use the `mcp_local-worldbank_indicator` API to gather various economic indicators including population, inflation rate, and unemployment rate.
6. Compile the collected data into a structured JSON file with economic power rankings and development tiers for each country.
7. Save the final output to the specified file (e.g., `economic_snapshot.json`).

## Pitfalls

- Ensure all five API endpoints are successfully called for each country to avoid incomplete data.
- Calculate the economic power rank and development tier correctly based on defined criteria.
