---
name: Collect Weather Data for Four Cities with Historical Data
description: Collect weather data for four specified cities using four API endpoints each for coordinates, hourly forecasts, daily forecasts, and historical data, and compile it into a JSON report with global summary statistics.
---

# Collect Weather Data for Four Cities with Historical Data

## When to use

Use this skill when needing to gather comprehensive weather information, including historical data, for four cities at once.

## Steps

1. Call `weather_get_coordinates` for each city to retrieve latitude, longitude, and timezone.
2. Call `weather_get_hourly` for each city using their coordinates to get an hourly forecast for the next 168 hours.
3. Call `weather_get_daily` for each city to retrieve a 14-day forecast.
4. Call `weather_get_historical` for each city to get a 30-day history of weather data.
5. Compile the results into a JSON format including a global summary of statistics for all four cities.

## Pitfalls

- Ensure that `weather_get_coordinates` is called first, as subsequent API calls depend on the results from this initial step.
- Make sure to handle the data retrieval in order to avoid overwriting or data mismatch across different calls.
