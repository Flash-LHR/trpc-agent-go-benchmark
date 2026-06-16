---
name: Create Economic Snapshot for Four Countries
description: Create economic snapshots for four specified major economies using four API endpoints each for comprehensive economic overview, GDP data, various economic indicators, and population data, while calculating economic power rank and development tier.
---

# Create Economic Snapshot for Four Countries

## When to use

When needing to compile economic snapshots for four countries with detailed economic data.

## Steps

1. Define the list of four major economies.
2. Use the mcp_local-worldbank_economic_snapshot API to gather comprehensive economic overviews.
3. Use the mcp_local-worldbank_gdp API to retrieve GDP data.
4. Use the mcp_local-worldbank_indicator API for additional economic indicators like GDP per capita, inflation, and unemployment rates.
5. Use the mcp_local-worldbank_population API for population data.
6. Calculate economic power rank and development tier based on collected data.
7. Save the results to economic_snapshot.json.

## Pitfalls

- Ensure all four countries are specified correctly.
- Check that all necessary API calls are made to avoid incomplete data.
- Verify calculations for economic power rank and development tier are performed correctly.
