---
name: Create Recipe Cookbook for Five International Dishes
description: Create a recipe cookbook by collecting data for five specified international dishes using five API endpoints per dish including searching meals by name, fetching full recipe details, filtering meals by category, area, and ingredient, while calculating difficulty ratings and estimated cooking times.
---

# Create Recipe Cookbook for Five International Dishes

## When to use

Use this skill when you need to create a comprehensive recipe cookbook with detailed dish information for five international recipes.

## Steps

1. Collect names and cuisines of five international dishes.
2. For each dish, call the meal search API to find the meal by name.
3. Fetch full meal details using the meal details API for information on ingredients and instructions.
4. Fetch meals by category, area, and ingredient using respective APIs.
5. Calculate difficulty ratings and estimated cooking times.
6. Compile all collected data into a structured JSON report and save it.

## Pitfalls

- Ensure the dish names used for search are spelled correctly to avoid zero results.
- Be aware of API rate limits; multiple calls in a short time can lead to temporary bans.
- Each dish must have complete information from all necessary APIs for accurate reporting.
