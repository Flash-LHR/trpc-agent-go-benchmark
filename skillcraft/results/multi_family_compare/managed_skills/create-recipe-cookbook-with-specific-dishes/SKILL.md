---
name: Create Recipe Cookbook with Specific Dishes
description: Create a recipe cookbook for a specified number of international dishes using three API endpoints each for detailed recipes, meals by area, and meals by ingredient, while calculating difficulty ratings and cooking times.
---

# Create Recipe Cookbook with Specific Dishes

## When to use

When tasked to compile a recipe collection from multiple APIs for a specific set of international dishes.

## Steps

1. Identify the dishes and corresponding search names and cuisines.
2. Use the meal details API for each dish to retrieve full recipes and instructions.
3. Use the meal search API to list meals by area for each specified cuisine.
4. Use the meal search API to list meals using specific ingredients for each dish.
5. Calculate difficulty rating and cooking time for each dish.
6. Compile results into a JSON format and save them to the required output file.

## Pitfalls

- Ensure all API calls return valid data; handle cases where the dish might not be found.
- Correctly format the output JSON to match the specified structure, including summary statistics.
