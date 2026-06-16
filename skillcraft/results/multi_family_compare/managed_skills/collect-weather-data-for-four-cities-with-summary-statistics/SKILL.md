---
name: Collect Weather Data for Four Cities with Summary Statistics
description: Collect weather data for four specified cities using four API endpoints each for coordinates, current weather, hourly forecasts, and daily forecasts, and compile it into a JSON report with global summary statistics.
---

# Collect Weather Data for Four Cities with Summary Statistics

## When to use

Use this skill when you need to gather comprehensive weather data for four cities and require a detailed summary of the findings.

## Steps

1. Call the `weather_get_coordinates` API for each city to obtain their latitude, longitude, and timezone as the first step.
2. Call the `weather_get_current` API for each city using the coordinates obtained to retrieve the current weather conditions.
3. Call the `weather_get_hourly` API for each city to get a 168-hour forecast.
4. Call the `weather_get_daily` API for each city to retrieve a 14-day forecast.
5. Compile the data into a JSON report, including details for each city and compute global summary statistics.

## Pitfalls

- Ensure to follow the fixed order of API calls where coordinates must be retrieved before current, hourly, and daily weather data.
- Be cautious about the limits of each API to avoid exceeding request limits or data quotas.
