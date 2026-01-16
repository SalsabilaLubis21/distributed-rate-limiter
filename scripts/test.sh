#!/bin/bash

# Test anonymous user
echo "Testing anonymous user..."
for i in {1..15}; do
  curl -s -o /dev/null -w "%{http_code}" -H "user_id: anon_user" http://localhost:8080/protected
  echo " - Request $i"
done

echo ""

# Test free user
echo "Testing free user..."
for i in {1..15}; do
  curl -s -o /dev/null -w "%{http_code}" -H "user_id: free_user" -H "X-User-Tier: free" http://localhost:8080/protected
  echo " - Request $i"
done

echo ""

# Test premium user
echo "Testing premium user..."
for i in {1..15}; do
  curl -s -o /dev/null -w "%{http_code}" -H "user_id: premium_user" -H "X-User-Tier: premium" http://localhost:8080/protected
  echo " - Request $i"
done