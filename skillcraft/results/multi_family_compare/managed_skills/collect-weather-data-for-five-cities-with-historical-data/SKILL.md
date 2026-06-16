---
name: Collect Weather Data for Five Cities with Historical Data
description: Collect weather data for five specified cities using five API endpoints each for coordinates, current weather conditions, hourly forecasts, daily forecasts, and historical data, and compile it into a JSON report with global summary statistics.
---

# Collect Weather Data for Five Cities with Historical Data

## When to use

Use this skill when needing comprehensive weather data for five cities with an analysis of historical trends.

## Steps

1. Use the 'weather_get_coordinates' tool to fetch latitude, longitude, and timezone for each city. This must be the first step for each city.
2. Use the 'weather_get_current' tool to obtain current weather conditions for each city using the coordinates.
3. Use the 'weather_get_hourly' tool to collect the 168-hour weather forecast for each city.
4. Use the 'weather_get_daily' tool to gather 14-day weather forecasts for each city.
5. Use the 'weather_get_historical' tool to obtain historical weather data for the last 30 days for each city.
6. Compile the collected data into a structured JSON format including global summary statistics.

## Pitfalls

- Ensure 'weather_get_coordinates' is always the first tool called for each city to avoid errors.
- Verify that the correct number of API calls is made for each city's data to avoid missing information.
