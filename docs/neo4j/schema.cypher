// Neo4j schema constraints and indexes for graph-first architecture.

CREATE CONSTRAINT action_graph_id_version IF NOT EXISTS
FOR (g:ActionGraph) REQUIRE (g.id, g.version) IS UNIQUE;

CREATE CONSTRAINT step_id IF NOT EXISTS
FOR (s:Step) REQUIRE s.id IS UNIQUE;

CREATE CONSTRAINT terminal_id IF NOT EXISTS
FOR (t:Terminal) REQUIRE t.id IS UNIQUE;

CREATE CONSTRAINT condition_id IF NOT EXISTS
FOR (c:Condition) REQUIRE c.id IS UNIQUE;

CREATE CONSTRAINT state_definition_id_version IF NOT EXISTS
FOR (d:StateDefinition) REQUIRE (d.id, d.version) IS UNIQUE;

CREATE CONSTRAINT state_name IF NOT EXISTS
FOR (s:State) REQUIRE s.name IS UNIQUE;

CREATE CONSTRAINT agent_id IF NOT EXISTS
FOR (a:Agent) REQUIRE a.id IS UNIQUE;

CREATE CONSTRAINT robot_id IF NOT EXISTS
FOR (r:Robot) REQUIRE r.id IS UNIQUE;

CREATE CONSTRAINT deployment_id IF NOT EXISTS
FOR (d:Deployment) REQUIRE d.id IS UNIQUE;

CREATE CONSTRAINT execution_id IF NOT EXISTS
FOR (e:Execution) REQUIRE e.id IS UNIQUE;

CREATE CONSTRAINT step_execution_id IF NOT EXISTS
FOR (s:StepExecution) REQUIRE s.id IS UNIQUE;

CREATE CONSTRAINT action_command_id IF NOT EXISTS
FOR (c:ActionCommand) REQUIRE c.id IS UNIQUE;

CREATE INDEX robot_state IF NOT EXISTS FOR (r:Robot) ON (r.state);
CREATE INDEX robot_last_seen IF NOT EXISTS FOR (r:Robot) ON (r.last_seen);
CREATE INDEX graph_id IF NOT EXISTS FOR (g:ActionGraph) ON (g.id);
CREATE INDEX execution_state IF NOT EXISTS FOR (e:Execution) ON (e.state);
CREATE INDEX deployment_status IF NOT EXISTS FOR (d:Deployment) ON (d.status);
