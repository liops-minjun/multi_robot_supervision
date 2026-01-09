#!/bin/bash

# Reset Neo4j data (destructive)
# Usage: NEO4J_CONTAINER=fleet-neo4j ./scripts/reset_neo4j.sh

set -e

NEO4J_CONTAINER="${NEO4J_CONTAINER:-fleet-neo4j}"
NEO4J_USER="${NEO4J_USER:-neo4j}"
NEO4J_PASSWORD="${NEO4J_PASSWORD:-neo4j123}"

echo "Resetting Neo4j data in container: ${NEO4J_CONTAINER}"
docker exec "${NEO4J_CONTAINER}" cypher-shell -u "${NEO4J_USER}" -p "${NEO4J_PASSWORD}" "MATCH (n) DETACH DELETE n;"
echo "Neo4j data reset complete"
