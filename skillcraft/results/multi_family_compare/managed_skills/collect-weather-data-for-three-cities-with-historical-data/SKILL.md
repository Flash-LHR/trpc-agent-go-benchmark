---
name: Collect Weather Data for Three Cities with Historical Data
description: Collect weather data for three specified cities using three API endpoints each for coordinates, daily forecasts, and historical data, and compile it into a JSON report with global summary statistics.
---

# Collect Weather Data for Three Cities with Historical Data

## When to use

When needing detailed weather data for three cities including historical context along with daily forecasts.

## Steps

1. Define the three cities for data collection along with their latitude and longitude.
2. Use the `local-weather_get_coordinates` tool first to get the coordinates for each city.
3. Use the `local-weather_get_daily` tool to get the daily forecast for each city.
4. Use the `local-weather_get_historical` tool to collect 30 days of historical data for each city.
5. Compile the data into a structured JSON format including global summary statistics for all cities.
6. Save the compiled data to a specified output file.

## Pitfalls

- Ensure the correct order of API calls: `weather_get_coordinates` must be first.
- Handle potential API errors or timeouts when retrieving historical data.
- Ensure the final JSON structure adheres to the required output format.
