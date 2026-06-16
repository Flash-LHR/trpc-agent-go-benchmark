---
name: Collect Weather Data for Three Cities with Summary Statistics
description: Collect weather data for three specified cities using three API endpoints each for coordinates, hourly forecasts, and daily forecasts, and compile it into a JSON report with global summary statistics.
---

# Collect Weather Data for Three Cities with Summary Statistics

## When to use

Use when needing to compare weather conditions across three cities and generate a detailed report.

## Steps

1. Call `weather_get_coordinates` for each city to get latitude, longitude, and timezone.
2. Call `weather_get_hourly` for each city using the coordinates retrieved.
3. Call `weather_get_daily` for each city using the coordinates retrieved.
4. Compile results into a structured JSON format as specified.
5. Include global summary statistics such as warmest and coldest city.

## Pitfalls

- Ensure `weather_get_coordinates` is called first before the other weather tools.
- Verify that all required fields are populated in the final JSON output.
- Double-check the calculations for summary statistics to ensure accuracy.
