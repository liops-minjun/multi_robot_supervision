#!/bin/bash
# Local Development Environment Variables
# Usage: source scripts/env_local.sh

# Database (Neo4j)
export NEO4J_URI=neo4j://localhost:7687
export NEO4J_USER=neo4j
export NEO4J_PASSWORD=neo4j123
export NEO4J_DATABASE=neo4j

# Server Ports
export HTTP_PORT=8081
export GRPC_PORT=9090

echo "Environment variables set:"
echo "  DB: neo4j://$NEO4J_USER:****@localhost:7687/$NEO4J_DATABASE"
echo "  HTTP: :$HTTP_PORT"
echo "  gRPC/QUIC: :$GRPC_PORT"
