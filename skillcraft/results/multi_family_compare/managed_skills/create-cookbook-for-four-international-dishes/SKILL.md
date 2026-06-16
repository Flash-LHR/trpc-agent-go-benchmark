---
name: Create Cookbook for Four International Dishes
description: Create a recipe cookbook by collecting data for four specified international dishes using four API endpoints per dish including searching meals by name, filtering by category, filtering by area, and filtering by ingredient. Additionally, calculate difficulty ratings and estimated cooking times for each dish.
---

# Create Cookbook for Four International Dishes

## When to use

When tasked with compiling a cookbook of four international dishes using respective APIs to gather detailed cooking data.

## Steps

1. Define the international dishes to include in the cookbook.
2. Use the 'search' API endpoint for each dish to find meals by name.
3. Use the 'by category' API to list meals in the category of each dish.
4. Use the 'by area' API to list meals by the cuisine region of each dish.
5. Use the 'by ingredient' API to find additional meals that use key ingredients from the dishes.
6. Retrieve detailed recipes and instruction data using the 'meal details' API for each dish.
7. Calculate difficulty ratings and estimated cooking times based on details gathered.
8. Compile the results into a structured JSON format and save as 'recipe_cookbook.json'.

## Pitfalls

- Ensure that the number of dishes and dishes' details match the task specification exactly.
- Make sure to handle potential API response errors appropriately.
- Check that the final output follows the required JSON structure for the cookbook.
