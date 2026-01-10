// Copyright 2026 Multi-Robot Supervision System
// Protocol Message Handler Implementation

#include "fleet_agent/protocol/message_handler.hpp"
#include "fleet_agent/core/logger.hpp"
#include "fleet_agent/transport/quic_transport.hpp"
#include "fleet_agent/executor/command_processor.hpp"

#include "fleet/v1/service.pb.h"
#include "fleet/v1/commands.pb.h"
#include "fleet/v1/graphs.pb.h"
#include <nlohmann/json.hpp>
#include <chrono>
#include <cstring>

namespace fleet_agent {
namespace protocol {

namespace {
logging::ComponentLogger log("MessageHandler");

int64_t now_ms() {
    return std::chrono::duration_cast<std::chrono::milliseconds>(
        std::chrono::system_clock::now().time_since_epoch()
    ).count();
}

fleet::v1::VertexType parse_vertex_type(const std::string& type) {
    if (type == "step") {
        return fleet::v1::VERTEX_TYPE_STEP;
    }
    if (type == "terminal") {
        return fleet::v1::VERTEX_TYPE_TERMINAL;
    }
    return fleet::v1::VERTEX_TYPE_UNKNOWN;
}

fleet::v1::StepType parse_step_type(const std::string& type) {
    if (type == "action") {
        return fleet::v1::STEP_TYPE_ACTION;
    }
    if (type == "wait") {
        return fleet::v1::STEP_TYPE_WAIT;
    }
    if (type == "condition") {
        return fleet::v1::STEP_TYPE_CONDITION;
    }
    return fleet::v1::STEP_TYPE_UNKNOWN;
}

fleet::v1::TerminalType parse_terminal_type(const std::string& type) {
    if (type == "success") {
        return fleet::v1::TERMINAL_TYPE_SUCCESS;
    }
    if (type == "failure") {
        return fleet::v1::TERMINAL_TYPE_FAILURE;
    }
    return fleet::v1::TERMINAL_TYPE_UNKNOWN;
}

fleet::v1::EdgeType parse_edge_type(const std::string& type) {
    if (type == "on_success") {
        return fleet::v1::EDGE_TYPE_ON_SUCCESS;
    }
    if (type == "on_failure") {
        return fleet::v1::EDGE_TYPE_ON_FAILURE;
    }
    if (type == "on_timeout") {
        return fleet::v1::EDGE_TYPE_ON_TIMEOUT;
    }
    if (type == "conditional") {
        return fleet::v1::EDGE_TYPE_CONDITIONAL;
    }
    return fleet::v1::EDGE_TYPE_UNKNOWN;
}

void parse_start_conditions(
    const nlohmann::json& conditions,
    fleet::v1::StepVertex* step) {

    if (!conditions.is_array() || !step) {
        return;
    }

    for (const auto& cond : conditions) {
        if (!cond.is_object()) {
            continue;
        }

        auto* out = step->add_start_conditions();
        if (cond.contains("id") && cond["id"].is_string()) {
            out->set_id(cond["id"].get<std::string>());
        }
        if (cond.contains("operator") && cond["operator"].is_string()) {
            out->set_operator_(cond["operator"].get<std::string>());
        }
        if (cond.contains("quantifier") && cond["quantifier"].is_string()) {
            out->set_quantifier(cond["quantifier"].get<std::string>());
        }
        if (cond.contains("target_type") && cond["target_type"].is_string()) {
            out->set_target_type(cond["target_type"].get<std::string>());
        }
        if (cond.contains("agent_id") && cond["agent_id"].is_string()) {
            out->set_agent_id(cond["agent_id"].get<std::string>());
        }
        if (cond.contains("agent_id") && cond["agent_id"].is_string()) {
            out->set_agent_id(cond["agent_id"].get<std::string>());
        }
        if (cond.contains("state") && cond["state"].is_string()) {
            out->set_state(cond["state"].get<std::string>());
        }
        if (cond.contains("state_operator") && cond["state_operator"].is_string()) {
            out->set_state_operator(cond["state_operator"].get<std::string>());
        }
        if (cond.contains("allowed_states") && cond["allowed_states"].is_array()) {
            for (const auto& state : cond["allowed_states"]) {
                if (state.is_string()) {
                    out->add_allowed_states(state.get<std::string>());
                }
            }
        }
        if (cond.contains("max_staleness_sec") && cond["max_staleness_sec"].is_number()) {
            out->set_max_staleness_sec(cond["max_staleness_sec"].get<float>());
        }
        if (cond.contains("require_online") && cond["require_online"].is_boolean()) {
            out->set_require_online(cond["require_online"].get<bool>());
        }
        if (cond.contains("message") && cond["message"].is_string()) {
            out->set_message(cond["message"].get<std::string>());
        }
    }
}

bool parse_canonical_graph_json(
    const std::string& json_str,
    fleet::v1::ActionGraph* graph,
    std::string* error) {

    if (!graph) {
        if (error) {
            *error = "graph output is null";
        }
        return false;
    }

    nlohmann::json root;
    try {
        root = nlohmann::json::parse(json_str);
    } catch (const std::exception& e) {
        if (error) {
            *error = std::string("invalid JSON: ") + e.what();
        }
        return false;
    }

    if (!root.is_object()) {
        if (error) {
            *error = "graph JSON must be an object";
        }
        return false;
    }

    graph->set_graph_json(json_str);

    if (root.contains("schema_version") && root["schema_version"].is_string()) {
        graph->set_schema_version(root["schema_version"].get<std::string>());
    }

    if (root.contains("action_graph") && root["action_graph"].is_object()) {
        const auto& meta = root["action_graph"];
        auto* out_meta = graph->mutable_metadata();
        if (meta.contains("id") && meta["id"].is_string()) {
            out_meta->set_id(meta["id"].get<std::string>());
        }
        if (meta.contains("name") && meta["name"].is_string()) {
            out_meta->set_name(meta["name"].get<std::string>());
        }
        if (meta.contains("version") && meta["version"].is_number_integer()) {
            out_meta->set_version(meta["version"].get<int>());
        }
        if (meta.contains("description") && meta["description"].is_string()) {
            out_meta->set_description(meta["description"].get<std::string>());
        }
    }

    if (root.contains("entry_point") && root["entry_point"].is_string()) {
        graph->set_entry_point(root["entry_point"].get<std::string>());
    }
    if (root.contains("checksum") && root["checksum"].is_string()) {
        graph->set_checksum(root["checksum"].get<std::string>());
    }

    if (root.contains("vertices") && root["vertices"].is_array()) {
        for (const auto& vertex_json : root["vertices"]) {
            if (!vertex_json.is_object()) {
                continue;
            }

            auto* vertex = graph->add_vertices();
            if (vertex_json.contains("id") && vertex_json["id"].is_string()) {
                vertex->set_id(vertex_json["id"].get<std::string>());
            }

            std::string type_str;
            if (vertex_json.contains("type") && vertex_json["type"].is_string()) {
                type_str = vertex_json["type"].get<std::string>();
            }
            vertex->set_type(parse_vertex_type(type_str));

            if (vertex->type() == fleet::v1::VERTEX_TYPE_STEP) {
                auto* step = vertex->mutable_step();

                if (vertex_json.contains("name") && vertex_json["name"].is_string()) {
                    step->set_name(vertex_json["name"].get<std::string>());
                }

                if (vertex_json.contains("step") && vertex_json["step"].is_object()) {
                    const auto& step_json = vertex_json["step"];
                    std::string step_type;
                    if (step_json.contains("step_type") && step_json["step_type"].is_string()) {
                        step_type = step_json["step_type"].get<std::string>();
                    }
                    step->set_step_type(parse_step_type(step_type));

                    if (step_json.contains("action") && step_json["action"].is_object()) {
                        const auto& action_json = step_json["action"];
                        auto* action = step->mutable_action();

                        if (action_json.contains("type") && action_json["type"].is_string()) {
                            action->set_action_type(action_json["type"].get<std::string>());
                        }
                        if (action_json.contains("server") && action_json["server"].is_string()) {
                            action->set_action_server(action_json["server"].get<std::string>());
                        }
                        if (action_json.contains("timeout_sec") && action_json["timeout_sec"].is_number()) {
                            action->set_timeout_sec(action_json["timeout_sec"].get<float>());
                        }
                        if (action_json.contains("params") && action_json["params"].is_object()) {
                            nlohmann::json params_payload = action_json["params"];
                            if (params_payload.contains("data")) {
                                params_payload = params_payload["data"];
                            }
                            action->set_goal_params(params_payload.dump());
                        }
                    }

                    if (step_json.contains("wait") && step_json["wait"].is_object()) {
                        const auto& wait_json = step_json["wait"];
                        auto* wait = step->mutable_wait();

                        if (wait_json.contains("timeout_sec") && wait_json["timeout_sec"].is_number()) {
                            wait->set_duration_sec(wait_json["timeout_sec"].get<float>());
                        }
                        if (wait_json.contains("condition") && wait_json["condition"].is_string()) {
                            wait->set_condition(wait_json["condition"].get<std::string>());
                        }
                    }

                    if (step_json.contains("condition") && step_json["condition"].is_object()) {
                        const auto& cond_json = step_json["condition"];
                        auto* cond = step->mutable_condition();

                        if (cond_json.contains("expression") && cond_json["expression"].is_string()) {
                            cond->set_expression(cond_json["expression"].get<std::string>());
                        }
                        if (cond_json.contains("branches") && cond_json["branches"].is_object()) {
                            for (const auto& [condition, next] : cond_json["branches"].items()) {
                                if (!next.is_string()) {
                                    continue;
                                }
                                auto* branch = cond->add_branches();
                                branch->set_condition(condition);
                                branch->set_next_vertex_id(next.get<std::string>());
                            }
                        }
                    }

                    if (step_json.contains("start_conditions")) {
                        parse_start_conditions(step_json["start_conditions"], step);
                    }

                    if (step_json.contains("states") && step_json["states"].is_object()) {
                        const auto& states = step_json["states"];
                        if (states.contains("pre") && states["pre"].is_array()) {
                            for (const auto& state : states["pre"]) {
                                if (state.is_string()) {
                                    step->add_pre_states(state.get<std::string>());
                                }
                            }
                        }
                        if (states.contains("during") && states["during"].is_array()) {
                            for (const auto& state : states["during"]) {
                                if (state.is_string()) {
                                    step->add_during_states(state.get<std::string>());
                                }
                            }
                        }
                        if (states.contains("success") && states["success"].is_array()) {
                            for (const auto& state : states["success"]) {
                                if (state.is_string()) {
                                    step->add_success_states(state.get<std::string>());
                                }
                            }
                        }
                        if (states.contains("failure") && states["failure"].is_array()) {
                            for (const auto& state : states["failure"]) {
                                if (state.is_string()) {
                                    step->add_failure_states(state.get<std::string>());
                                }
                            }
                        }
                    }
                } else {
                    step->set_step_type(fleet::v1::STEP_TYPE_UNKNOWN);
                }
            } else if (vertex->type() == fleet::v1::VERTEX_TYPE_TERMINAL) {
                if (vertex_json.contains("terminal") && vertex_json["terminal"].is_object()) {
                    const auto& terminal_json = vertex_json["terminal"];
                    auto* terminal = vertex->mutable_terminal();

                    if (terminal_json.contains("terminal_type") && terminal_json["terminal_type"].is_string()) {
                        terminal->set_terminal_type(
                            parse_terminal_type(terminal_json["terminal_type"].get<std::string>()));
                    }
                    if (terminal_json.contains("message") && terminal_json["message"].is_string()) {
                        terminal->set_message(terminal_json["message"].get<std::string>());
                    }
                }
            }
        }
    }

    if (root.contains("edges") && root["edges"].is_array()) {
        for (const auto& edge_json : root["edges"]) {
            if (!edge_json.is_object()) {
                continue;
            }

            auto* edge = graph->add_edges();
            if (edge_json.contains("from") && edge_json["from"].is_string()) {
                edge->set_from_vertex(edge_json["from"].get<std::string>());
            } else if (edge_json.contains("from_vertex") && edge_json["from_vertex"].is_string()) {
                edge->set_from_vertex(edge_json["from_vertex"].get<std::string>());
            }

            if (edge_json.contains("to") && edge_json["to"].is_string()) {
                edge->set_to_vertex(edge_json["to"].get<std::string>());
            } else if (edge_json.contains("to_vertex") && edge_json["to_vertex"].is_string()) {
                edge->set_to_vertex(edge_json["to_vertex"].get<std::string>());
            }

            std::string edge_type;
            if (edge_json.contains("type") && edge_json["type"].is_string()) {
                edge_type = edge_json["type"].get<std::string>();
            }
            edge->set_type(parse_edge_type(edge_type));

            if (edge_json.contains("condition") && edge_json["condition"].is_string()) {
                edge->set_condition(edge_json["condition"].get<std::string>());
            } else if (edge_json.contains("config") && edge_json["config"].is_object()) {
                const auto& config = edge_json["config"];
                if (config.contains("condition") && config["condition"].is_string()) {
                    edge->set_condition(config["condition"].get<std::string>());
                }
            }
        }
    }

    if (graph->metadata().id().empty()) {
        if (error) {
            *error = "missing action_graph.id in canonical payload";
        }
        return false;
    }

    if (graph->entry_point().empty()) {
        if (error) {
            *error = "missing entry_point in canonical payload";
        }
        return false;
    }

    return true;
}
}  // namespace

// ============================================================
// MessageHandler Implementation
// ============================================================

MessageHandler::MessageHandler(const Dependencies& deps)
    : deps_(deps) {
    log.info("Message handler initialized for agent {}", deps_.agent_id);
}

MessageHandler::~MessageHandler() = default;

MessageHandler::HandleResult MessageHandler::handle(const fleet::v1::ServerMessage& message) {
    log.debug("Handling server message: id={}, seq={}",
              message.message_id(), message.sequence());

    // Dispatch based on payload type
    switch (message.payload_case()) {
        case fleet::v1::ServerMessage::kExecute:
            return handle_execute_command(message.execute());

        case fleet::v1::ServerMessage::kCancel:
            return handle_cancel_command(message.cancel());

        case fleet::v1::ServerMessage::kConfigUpdate:
            return handle_config_update(message.config_update());

        case fleet::v1::ServerMessage::kPing:
            return handle_ping(message.ping());

        case fleet::v1::ServerMessage::kDeployGraph:
            return handle_deploy_graph(message.deploy_graph());

        case fleet::v1::ServerMessage::kAck:
            // Server acknowledgment - just log
            log.debug("Received server ack for message: {}",
                     message.ack().acked_message_id());
            return HandleResult{true, "", nullptr};

        case fleet::v1::ServerMessage::PAYLOAD_NOT_SET:
            log.warn("Received message with no payload");
            return HandleResult{false, "No payload in message", nullptr};

        default:
            log.warn("Unknown message payload type: {}",
                    static_cast<int>(message.payload_case()));
            return HandleResult{false, "Unknown message type", nullptr};
    }
}

MessageHandler::HandleResult MessageHandler::handle_execute_command(
    const fleet::v1::ExecuteCommand& cmd) {

    log.info("Execute command: robot={}, action={}, command_id={}",
             cmd.agent_id(), cmd.action_type(), cmd.command_id());

    if (deps_.command_processor) {
        deps_.command_processor->enqueue_execute_command(cmd, cmd.command_id());
        return HandleResult{true, "", nullptr};
    }

    // Legacy fallback: queue command for processing
    if (deps_.command_queue) {
        ActionRequest request;
        request.command_id = cmd.command_id();
        request.agent_id = cmd.agent_id();
        request.task_id = cmd.task_id();
        request.step_id = cmd.step_id();
        request.action_type = cmd.action_type();
        request.action_server = cmd.action_server();
        request.params_json = std::string(cmd.params().begin(), cmd.params().end());
        request.timeout_sec = cmd.timeout_sec();
        request.deadline_ms = cmd.deadline_ms();

        deps_.command_queue->push(std::move(request));
    }

    return HandleResult{true, "", nullptr};
}

MessageHandler::HandleResult MessageHandler::handle_cancel_command(
    const fleet::v1::CancelCommand& cmd) {

    log.info("Cancel command: robot={}, task={}, reason={}",
             cmd.agent_id(), cmd.task_id(), cmd.reason());

    // Forward to command processor for cancellation
    if (deps_.command_processor) {
        deps_.command_processor->cancel_action(cmd.agent_id(), cmd.reason());
    }

    return HandleResult{true, "", nullptr};
}

MessageHandler::HandleResult MessageHandler::handle_config_update(
    const fleet::v1::ConfigUpdate& update) {

    log.info("Config update: robot={}, version={}",
             update.agent_id(), update.version());

    // Parse state definition from bytes
    auto state_def = parse_state_definition(update);
    if (!state_def) {
        log.error("Failed to parse state definition for agent {}", update.agent_id());
        auto response = build_config_ack(
            update.agent_id(), update.version(),
            false, "Failed to parse state definition"
        );
        send_response(response);
        return HandleResult{false, "Failed to parse state definition", response};
    }

    // Store state definition
    if (deps_.state_storage) {
        deps_.state_storage->store(*state_def);
        deps_.state_storage->map_agent(update.agent_id(), state_def->id);
    }

    // Configure state tracker
    if (deps_.state_tracker_mgr) {
        deps_.state_tracker_mgr->configure_agent(update.agent_id(), *state_def);
    }

    // Send acknowledgment
    auto response = build_config_ack(
        update.agent_id(), state_def->version,
        true, ""
    );
    send_response(response);

    log.info("Applied state definition {} (v{}) to agent {}",
             state_def->id, state_def->version, update.agent_id());

    return HandleResult{true, "", response};
}

MessageHandler::HandleResult MessageHandler::handle_ping(
    const fleet::v1::PingRequest& ping) {

    log.debug("Ping received: id={}, timestamp={}",
              ping.ping_id(), ping.timestamp_ms());

    auto response = build_pong_response(ping.ping_id(), ping.timestamp_ms());
    send_response(response);

    return HandleResult{true, "", response};
}

MessageHandler::HandleResult MessageHandler::handle_deploy_graph(
    const fleet::v1::DeployGraphRequest& deploy) {

    const auto& graph_msg = deploy.graph();
    fleet::v1::ActionGraph graph;

    if (graph_msg.metadata().id().empty() && !graph_msg.graph_json().empty()) {
        std::string parse_error;
        if (!parse_canonical_graph_json(graph_msg.graph_json(), &graph, &parse_error)) {
            log.error("Failed to parse canonical graph JSON: {}", parse_error);
            auto response = build_deploy_response(
                deploy.correlation_id(),
                "",
                0,
                "",
                false,
                parse_error
            );
            send_response(response);
            return HandleResult{false, parse_error, response};
        }
    } else {
        graph = graph_msg;
    }

    if (graph.metadata().id().empty()) {
        auto response = build_deploy_response(
            deploy.correlation_id(),
            "",
            0,
            "",
            false,
            "Graph metadata.id is missing"
        );
        send_response(response);
        return HandleResult{false, "Graph metadata.id is missing", response};
    }

    log.info("Deploy graph: id={}, name={}, version={}, force={}",
             graph.metadata().id(), graph.metadata().name(),
             graph.metadata().version(), deploy.force());

    // Check if already exists (unless force)
    if (!deploy.force() && deps_.graph_storage &&
        deps_.graph_storage->exists(graph.metadata().id())) {

        auto existing_version = deps_.graph_storage->get_version(graph.metadata().id());
        if (existing_version && *existing_version >= graph.metadata().version()) {
            log.info("Graph {} already at version {} (requested {}), skipping",
                     graph.metadata().id(), *existing_version, graph.metadata().version());

            auto response = build_deploy_response(
                deploy.correlation_id(),
                graph.metadata().id(),
                *existing_version,
                graph.checksum(),
                true,
                ""
            );
            send_response(response);
            return HandleResult{true, "", response};
        }
    }

    // Store the graph
    bool stored = false;
    if (deps_.graph_storage) {
        stored = deps_.graph_storage->store(graph);
    }

    if (!stored) {
        log.error("Failed to store graph: {}", graph.metadata().id());
        auto response = build_deploy_response(
            deploy.correlation_id(),
            graph.metadata().id(),
            graph.metadata().version(),
            "",
            false,
            "Failed to store graph"
        );
        send_response(response);
        return HandleResult{false, "Failed to store graph", response};
    }

    // Send success response
    auto response = build_deploy_response(
        deploy.correlation_id(),
        graph.metadata().id(),
        graph.metadata().version(),
        graph.checksum(),
        true,
        ""
    );
    send_response(response);

    log.info("Successfully deployed graph: {} (v{})",
             graph.metadata().id(), graph.metadata().version());

    return HandleResult{true, "", response};
}

// ============================================================
// Response Builders
// ============================================================

std::shared_ptr<fleet::v1::AgentMessage> MessageHandler::build_deploy_response(
    const std::string& correlation_id,
    const std::string& graph_id,
    int version,
    const std::string& checksum,
    bool success,
    const std::string& error) {

    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(deps_.agent_id);
    msg->set_timestamp_ms(now_ms());

    auto* response = msg->mutable_deploy_response();
    response->set_correlation_id(correlation_id);
    response->set_success(success);
    response->set_graph_id(graph_id);
    response->set_deployed_version(version);
    response->set_checksum(checksum);
    if (!success) {
        response->set_error(error);
    }

    return msg;
}

std::shared_ptr<fleet::v1::AgentMessage> MessageHandler::build_pong_response(
    const std::string& ping_id,
    int64_t server_timestamp_ms) {

    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(deps_.agent_id);
    msg->set_timestamp_ms(now_ms());

    auto* pong = msg->mutable_pong();
    pong->set_ping_id(ping_id);
    pong->set_server_timestamp_ms(server_timestamp_ms);
    pong->set_agent_timestamp_ms(now_ms());

    return msg;
}

std::shared_ptr<fleet::v1::AgentMessage> MessageHandler::build_action_result(
    const ActionResultInternal& result) {

    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(deps_.agent_id);
    msg->set_timestamp_ms(now_ms());

    auto* action_result = msg->mutable_action_result();
    action_result->set_command_id(result.command_id);
    action_result->set_agent_id(result.agent_id);
    action_result->set_task_id(result.task_id);
    action_result->set_step_id(result.step_id);
    action_result->set_status(static_cast<fleet::v1::ActionStatus>(result.status));
    action_result->set_result(result.result_json);
    action_result->set_error(result.error);
    action_result->set_started_at_ms(result.started_at_ms);
    action_result->set_completed_at_ms(result.completed_at_ms);

    return msg;
}

std::shared_ptr<fleet::v1::AgentMessage> MessageHandler::build_config_ack(
    const std::string& agent_id,
    int version,
    bool success,
    const std::string& error) {

    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(deps_.agent_id);
    msg->set_timestamp_ms(now_ms());

    auto* ack = msg->mutable_config_ack();
    ack->set_agent_id(agent_id);
    ack->set_version(version);
    ack->set_success(success);
    if (!success && !error.empty()) {
        ack->set_error(error);
    }

    return msg;
}

bool MessageHandler::send_response(std::shared_ptr<fleet::v1::AgentMessage> response) {
    if (!response) return false;

    if (deps_.quic_outbound_queue) {
        OutboundMessage out;
        out.message = response;
        out.created_at = std::chrono::steady_clock::now();
        out.priority = 1;
        deps_.quic_outbound_queue->push(std::move(out));
        return true;
    }

    if (deps_.quic_client && deps_.quic_client->is_connected()) {
        // Serialize and send via QUIC
        std::string serialized;
        if (response->SerializeToString(&serialized)) {
            auto stream = deps_.quic_client->get_stream();
            if (stream) {
                // Length-prefixed framing (4-byte big-endian) to match server
                const uint32_t len = static_cast<uint32_t>(serialized.size());
                std::vector<uint8_t> data(4 + serialized.size());
                data[0] = static_cast<uint8_t>((len >> 24) & 0xFF);
                data[1] = static_cast<uint8_t>((len >> 16) & 0xFF);
                data[2] = static_cast<uint8_t>((len >> 8) & 0xFF);
                data[3] = static_cast<uint8_t>(len & 0xFF);
                std::memcpy(data.data() + 4, serialized.data(), serialized.size());

                bool sent = stream->write(
                    data.data(),
                    data.size(),
                    false  // keep stream open for reuse
                );
                if (sent) {
                    deps_.quic_client->release_stream(stream);
                } else {
                    stream->close();
                }
                return sent;
            }
        }
    }

    log.warn("Failed to send response - QUIC not connected");
    return false;
}

std::optional<state::StateDefinition> MessageHandler::parse_state_definition(
    const fleet::v1::ConfigUpdate& update) {

    if (update.state_definition().empty()) {
        return std::nullopt;
    }

    try {
        auto j = nlohmann::json::parse(
            reinterpret_cast<const char*>(update.state_definition().data()),
            reinterpret_cast<const char*>(update.state_definition().data()) +
                update.state_definition().size()
        );

        state::StateDefinition def = j.get<state::StateDefinition>();

        // Override version with proto field if provided
        if (update.version() > 0) {
            def.version = update.version();
        }

        return def;
    } catch (const std::exception& e) {
        log.error("Failed to parse state definition: {}", e.what());
        return std::nullopt;
    }
}

// ============================================================
// CapabilityRegistrar Implementation
// ============================================================

CapabilityRegistrar::CapabilityRegistrar(
    transport::QUICClient* quic_client,
    const std::string& agent_id)
    : quic_client_(quic_client)
    , agent_id_(agent_id) {
}

bool CapabilityRegistrar::register_capabilities(
    const std::string& agent_id,
    const std::vector<ActionCapability>& capabilities) {

    log.info("[DEBUG] register_capabilities called: robot={}, num_caps={}",
              agent_id, capabilities.size());

    // Debug: print each capability
    for (size_t i = 0; i < capabilities.size(); i++) {
        log.info("[DEBUG] Capability[{}]: type={}, server={}",
                  i, capabilities[i].action_type, capabilities[i].action_server);
    }

    auto msg = build_registration_message(agent_id, capabilities);
    if (!msg) {
        log.warn("[DEBUG] build_registration_message returned null");
        return false;
    }

    // Debug: verify protobuf message
    const auto& cap_reg = msg->capability_registration();
    log.info("[DEBUG] Protobuf message: agent_id={}, capabilities_size={}",
              cap_reg.agent_id(), cap_reg.capabilities_size());

    for (int i = 0; i < cap_reg.capabilities_size(); i++) {
        const auto& cap = cap_reg.capabilities(i);
        log.info("[DEBUG] Protobuf capability[{}]: type={}, server={}",
                  i, cap.action_type(), cap.action_server());
    }

    if (quic_client_ && quic_client_->is_connected()) {
        std::string serialized;
        if (msg->SerializeToString(&serialized)) {
            log.info("[DEBUG] Serialized protobuf size: {} bytes", serialized.size());

            // Build length-prefixed message (4-byte big-endian length + data)
            // Same framing as RegisterAgentRequest
            std::vector<uint8_t> data;
            data.reserve(4 + serialized.size());

            uint32_t len = static_cast<uint32_t>(serialized.size());
            data.push_back(static_cast<uint8_t>((len >> 24) & 0xFF));
            data.push_back(static_cast<uint8_t>((len >> 16) & 0xFF));
            data.push_back(static_cast<uint8_t>((len >> 8) & 0xFF));
            data.push_back(static_cast<uint8_t>(len & 0xFF));
            data.insert(data.end(), serialized.begin(), serialized.end());

            log.info("[DEBUG] Total data with length prefix: {} bytes", data.size());

            auto stream = quic_client_->get_stream();
            if (stream) {
                bool sent = stream->write(data.data(), data.size(), false);
                if (sent) {
                    quic_client_->release_stream(stream);
                } else {
                    stream->close();
                }

                if (sent) {
                    log.info("Registered {} capabilities for robot {} ({} bytes sent)",
                             capabilities.size(), agent_id, data.size());
                }
                return sent;
            }
        }
    }

    log.warn("Failed to register capabilities - QUIC not connected");
    return false;
}

bool CapabilityRegistrar::register_all(
    const std::vector<std::pair<std::string, std::vector<ActionCapability>>>& robot_capabilities) {

    bool all_success = true;
    for (const auto& [agent_id, caps] : robot_capabilities) {
        if (!register_capabilities(agent_id, caps)) {
            all_success = false;
        }
    }
    return all_success;
}

std::shared_ptr<fleet::v1::AgentMessage> CapabilityRegistrar::build_registration_message(
    const std::string& agent_id,
    const std::vector<ActionCapability>& capabilities) {

    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(agent_id_);
    msg->set_timestamp_ms(now_ms());

    // Use dedicated capability_registration message
    auto* registration = msg->mutable_capability_registration();
    registration->set_agent_id(agent_id);

    // Add capabilities
    for (const auto& cap : capabilities) {
        auto* proto_cap = registration->add_capabilities();
        proto_cap->set_action_type(cap.action_type);
        proto_cap->set_action_server(cap.action_server);
        proto_cap->set_package(cap.package);
        proto_cap->set_action_name(cap.action_name);
        proto_cap->set_goal_schema(cap.goal_schema_json);
        proto_cap->set_result_schema(cap.result_schema_json);
        proto_cap->set_feedback_schema(cap.feedback_schema_json);

        // Include availability status so server can track offline action servers
        proto_cap->set_is_available(cap.available.load());

        if (!cap.success_criteria.is_empty()) {
            auto* criteria = proto_cap->mutable_success_criteria();
            criteria->set_field(cap.success_criteria.field);
            criteria->set_operator_(cap.success_criteria.op);
            criteria->set_value(cap.success_criteria.value);
        }
    }

    return msg;
}

}  // namespace protocol
}  // namespace fleet_agent
