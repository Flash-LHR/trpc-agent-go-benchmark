---
name: Create Recipe Cookbook with International Dishes
description: Collect and compile a recipe cookbook for a specified number of international dishes using different API endpoints for meal search, meal category, and meal area.
---

# Create Recipe Cookbook with International Dishes

## When to use

Use this skill when developing a recipe cookbook that includes multiple international dishes specified by name, cuisine, and category.

## Steps

1. Collect recipe data for each dish using meal search by name.
2. Retrieve additional recipes by category for each dish.
3. Retrieve additional recipes by area for each dish.
4. Compile collected data into a structured JSON format for the recipe cookbook.
5. Include details such as ingredient count, difficulty rating, cooking time, and a summary of recipes.

## Pitfalls

- Ensure that the correct API endpoints are called for each specific task (search, by category, by area).
- Make sure to handle errors when API calls fail (e.g., network errors, SSL errors).
- Validate that the compiled JSON format matches the required output structure.
