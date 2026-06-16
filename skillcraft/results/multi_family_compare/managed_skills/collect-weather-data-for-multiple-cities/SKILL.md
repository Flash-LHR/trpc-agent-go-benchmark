---
name: Collect Weather Data for Multiple Cities
description: Collect weather data for multiple cities using various APIs, including fetching coordinates, current weather, hourly forecasts, and daily forecasts, compiling the results into a structured JSON file.
---

# Collect Weather Data for Multiple Cities

## When to use

Use this skill to gather comprehensive weather information for multiple locations using defined APIs.

## Steps

1. Define the cities along with their latitude, longitude, and other necessary metadata.
2. Use the `local-weather_get_coordinates` API to fetch the coordinates for each city first.
3. Use the `local-weather_get_current` API to get current weather conditions for each city using the fetched coordinates.
4. Use the `local-weather_get_hourly` API to obtain the hourly weather forecasts for each city.
5. Use the `local-weather_get_daily` API to retrieve the daily weather forecasts for each city.
6. Compile the collected data into a JSON structure, including summary statistics for analysis.
7. Save the resulting JSON to a specified file (e.g., `weather_report.json`).

## Pitfalls

- Ensure to call the `weather_get_coordinates` API first before any other weather API.
- Pay attention to the specific API output structure to correctly store the required fields.
- Validate the JSON output before saving to prevent format errors.
