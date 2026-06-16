---
name: Create Recipe Cookbook for Four International Dishes
description: Create a recipe cookbook by collecting data for four specified international dishes using four API endpoints for searching meals by name, filtering meals by category, and filtering meals by area, while calculating difficulty ratings and estimated cooking times.
---

# Create Recipe Cookbook for Four International Dishes

## When to use

Use this skill to create a structured cookbook when tasked with compiling international dishes with specific requirements on dish count and APIs.

## Steps

1. Specify the list of four dishes with names and corresponding cuisines.
2. Use the meal search API to find meals by name for each dish.
3. Use the meal details API to get detailed recipes for each dish.
4. Use the meal by category API to fetch related meals within the same category as each dish.
5. Use the meal by area API to fetch related meals from the respective cuisine area for each dish.
6. Calculate the difficulty rating and estimated cooking time for each dish.
7. Compile the results into a JSON format specified in the task requirements.
8. Save the finalized cookbook to a specified output file.

## Pitfalls

- Ensure all API calls for meal data are made in the correct order to avoid missing dependencies.
- Check that the output JSON format matches the required structure exactly to prevent saving errors.
- Make sure to verify the accuracy of the difficulty ratings and estimated cooking times before finalizing the cookbook.
