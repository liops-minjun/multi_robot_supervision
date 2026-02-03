// Copyright 2026 Multi-Robot Supervision System
// Task Executor Implementation - Agent-driven graph execution

#include "robot_agent/executor/task_executor.hpp"
#include "robot_agent/core/logger.hpp"
#include "robot_agent/core/shutdown.hpp"
#include "robot_agent/state/state_tracker.hpp"

#include <algorithm>

#include <nlohmann/json.hpp>

namespace robot_agent {
namespace executor {

namespace {
logging::ComponentLogger log("TaskExecutor");

std::string task_status_str(TaskExecutor::TaskStatus status) {
    switch (status) {
        case TaskExecutor::TaskStatus::PENDING: return "PENDING";
        case TaskExecutor::TaskStatus::RUNNING: return "RUNNING";
        case TaskExecutor::TaskStatus::WAITING_PRECONDITION: return "WAITING_PRECONDITION";
        case TaskExecutor::TaskStatus::EXECUTING_ACTION: return "EXECUTING_ACTION";
        case TaskExecutor::TaskStatus::COMPLETED: return "COMPLETED";
        case TaskExecutor::TaskStatus::FAILED: return "FAILED";
        case TaskExecutor::TaskStatus::CANCELLED: return "CANCELLED";
    }
    return "UNKNOWN";
}
}  // namespace

// ============================================================
// Constructor / Destructor
// ============================================================

TaskExecutor::TaskExecutor(
    rclcpp::Node::SharedPtr node,
    const std::string& agent_id,
    graph::GraphExecutor& graph_executor,
    graph::GraphStorage& graph_storage,
    QuicOutboundQueue& outbound_queue,
    state::StateTrackerManager* state_tracker_mgr)
    : node_(node)
    , agent_id_(agent_id)
    , graph_executor_(graph_executor)
    , graph_storage_(graph_storage)
    , outbound_queue_(outbound_queue)
    , state_tracker_mgr_(state_tracker_mgr) {

    log.info("Initialized for agent {}", agent_id_);
}

TaskExecutor::~TaskExecutor() {
    stop();
}

// ============================================================
// Lifecycle
// ============================================================

void TaskExecutor::start() {
    if (running_.load()) {
        log.warn("Already running");
        return;
    }

    running_.store(true);
    executor_thread_ = std::thread(&TaskExecutor::execution_loop, this);

    log.info("Started execution thread");
}

void TaskExecutor::stop() {
    if (!running_.load()) {
        return;
    }

    running_.store(false);

    if (executor_thread_.joinable()) {
        executor_thread_.join();
    }

    log.info("Stopped");
}

// ============================================================
// Robot Management
// ============================================================

void TaskExecutor::add_robot(
    const std::string& robot_id,
    const std::string& ros_namespace,
    CapabilityStore& capabilities) {

    std::lock_guard<std::mutex> lock(executors_mutex_);

    if (executors_.find(robot_id) != executors_.end()) {
        log.warn("Robot {} already registered", robot_id);
        return;
    }

    // Create ActionExecutor with callback bound to this TaskExecutor
    auto executor = std::make_unique<ActionExecutor>(
        node_,
        robot_id,
        ros_namespace,
        capabilities,
        [this](const ActionResultInternal& result) {
            this->on_action_result(result);
        },
        nullptr  // feedback callback (optional)
    );

    executors_[robot_id] = std::move(executor);
    log.info("Added robot {} (namespace: {})", robot_id, ros_namespace);
}

void TaskExecutor::remove_robot(const std::string& robot_id) {
    std::lock_guard<std::mutex> lock(executors_mutex_);
    executors_.erase(robot_id);
    log.info("Removed robot {}", robot_id);
}

bool TaskExecutor::rename_robot(const std::string& old_id, const std::string& new_id) {
    std::lock_guard<std::mutex> lock(executors_mutex_);

    auto it = executors_.find(old_id);
    if (it == executors_.end()) {
        log.warn("Cannot rename robot {} to {} - old ID not found", old_id, new_id);
        return false;
    }

    if (executors_.find(new_id) != executors_.end()) {
        log.warn("Cannot rename robot {} to {} - new ID already exists", old_id, new_id);
        return false;
    }

    // Move the executor to the new key
    auto executor = std::move(it->second);
    executors_.erase(it);
    executors_[new_id] = std::move(executor);

    log.info("Renamed robot {} to {}", old_id, new_id);
    return true;
}

// ============================================================
// Task Control
// ============================================================

bool TaskExecutor::start_task(
    const std::string& task_id,
    const std::string& behavior_tree_id,
    const std::string& robot_id,
    const std::unordered_map<std::string, std::string>& params) {

    log.info("[TASK] ════════════════════════════════════════════════");
    log.info("[TASK] Starting task {} (behavior tree: {}, robot: {})",
             task_id, behavior_tree_id, robot_id);

    // Check if task already exists
    {
        std::lock_guard<std::mutex> lock(tasks_mutex_);
        if (tasks_.find(task_id) != tasks_.end()) {
            log.error("[TASK] Task {} already exists", task_id);
            return false;
        }
    }

    // Check if robot exists
    ActionExecutor* executor = get_executor(robot_id);
    if (!executor) {
        log.error("[TASK] Robot {} not found", robot_id);
        return false;
    }

    // Get behavior tree from storage
    auto behavior_tree_opt = graph_storage_.load(behavior_tree_id);
    if (!behavior_tree_opt) {
        log.error("[TASK] Behavior tree {} not found", behavior_tree_id);
        return false;
    }

    // Create running task
    RunningTask task;
    task.task_id = task_id;
    task.behavior_tree_id = behavior_tree_id;
    task.robot_id = robot_id;
    task.behavior_tree = *behavior_tree_opt;
    task.status = TaskStatus::RUNNING;
    task.started_at = std::chrono::steady_clock::now();
    task.last_state_report = task.started_at;

    // Initialize execution context
    task.ctx = graph_executor_.start_execution(task_id, robot_id, task.behavior_tree, params);

    log.info("[TASK]   Entry point: {}", task.ctx.current_vertex_id);
    log.info("[TASK]   Parameters: {}", params.size());
    for (const auto& [k, v] : params) {
        log.info("[TASK]     {}: {}", k, truncate(v, 60));
    }
    log.info("[TASK] ────────────────────────────────────────────────");

    // Store task and report initial state (within same lock scope to avoid race)
    {
        std::lock_guard<std::mutex> lock(tasks_mutex_);
        tasks_[task_id] = std::move(task);
        // Report initial state to server while still holding the lock
        report_state_to_server(tasks_[task_id]);
    }

    return true;
}

bool TaskExecutor::cancel_task(const std::string& task_id, const std::string& reason) {
    std::lock_guard<std::mutex> lock(tasks_mutex_);

    auto it = tasks_.find(task_id);
    if (it == tasks_.end()) {
        log.warn("Task {} not found for cancellation", task_id);
        return false;
    }

    RunningTask& task = it->second;

    // If action is executing, cancel it
    if (task.action_pending) {
        ActionExecutor* executor = get_executor(task.robot_id);
        if (executor && executor->is_executing()) {
            executor->cancel(reason);
        }
    }

    // Mark as cancelled
    task.status = TaskStatus::CANCELLED;
    task.blocking_reason = reason;

    log.info("[TASK] Cancelled task {}: {}", task_id, reason);

    // Report to server
    report_state_to_server(task);

    return true;
}

std::optional<TaskExecutor::TaskStatus> TaskExecutor::get_task_status(
    const std::string& task_id) const {

    std::lock_guard<std::mutex> lock(tasks_mutex_);
    auto it = tasks_.find(task_id);
    if (it == tasks_.end()) {
        return std::nullopt;
    }
    return it->second.status;
}

std::vector<std::string> TaskExecutor::get_running_task_ids() const {
    std::vector<std::string> ids;
    std::lock_guard<std::mutex> lock(tasks_mutex_);
    for (const auto& [id, task] : tasks_) {
        if (task.status == TaskStatus::RUNNING ||
            task.status == TaskStatus::WAITING_PRECONDITION ||
            task.status == TaskStatus::EXECUTING_ACTION) {
            ids.push_back(id);
        }
    }
    return ids;
}

// ============================================================
// Fleet State
// ============================================================

void TaskExecutor::update_fleet_state(
    const std::unordered_map<std::string, int>& robot_states,
    const std::unordered_map<std::string, bool>& robot_executing) {

    std::lock_guard<std::mutex> lock(fleet_state_mutex_);
    fleet_states_ = robot_states;
    fleet_executing_ = robot_executing;
}

// ============================================================
// Main Execution Loop
// ============================================================

void TaskExecutor::execution_loop() {
    log.info("Execution loop started");

    auto last_report = std::chrono::steady_clock::now();

    while (running_.load() && FLEET_AGENT_RUNNING) {
        auto loop_start = std::chrono::steady_clock::now();

        // Process all running tasks
        {
            std::lock_guard<std::mutex> lock(tasks_mutex_);

            for (auto& [task_id, task] : tasks_) {
                // Skip completed/cancelled tasks
                if (task.status == TaskStatus::COMPLETED ||
                    task.status == TaskStatus::FAILED ||
                    task.status == TaskStatus::CANCELLED) {
                    continue;
                }

                process_task(task);
            }
        }

        // Periodic state reporting
        if (loop_start - last_report > kStateReportInterval) {
            report_all_states();
            last_report = loop_start;
        }

        // Clean up completed tasks (keep for 10 seconds for status queries)
        {
            std::lock_guard<std::mutex> lock(tasks_mutex_);
            for (auto it = tasks_.begin(); it != tasks_.end(); ) {
                const auto& task = it->second;
                if ((task.status == TaskStatus::COMPLETED ||
                     task.status == TaskStatus::FAILED ||
                     task.status == TaskStatus::CANCELLED) &&
                    (loop_start - task.started_at > std::chrono::seconds(10))) {
                    it = tasks_.erase(it);
                } else {
                    ++it;
                }
            }
        }

        // Maintain tick interval
        auto elapsed = std::chrono::steady_clock::now() - loop_start;
        if (elapsed < kTickInterval) {
            std::this_thread::sleep_for(kTickInterval - elapsed);
        }
    }

    log.info("Execution loop stopped");
}

void TaskExecutor::process_task(RunningTask& task) {
    // Get current vertex
    auto vertex_opt = get_current_vertex(task);
    if (!vertex_opt) {
        complete_task(task, false, "Vertex not found: " + task.ctx.current_vertex_id);
        return;
    }

    const auto& vertex = *vertex_opt;

    // Check if terminal reached
    if (graph_executor_.is_terminal(vertex)) {
        bool success = (vertex.terminal().terminal_type() ==
                       fleet::v1::TERMINAL_TYPE_SUCCESS);
        complete_task(task, success);
        return;
    }

    // If action is pending (waiting for completion), skip
    if (task.action_pending) {
        return;
    }

    // Check precondition
    if (task.status == TaskStatus::RUNNING ||
        task.status == TaskStatus::WAITING_PRECONDITION) {

        if (!check_precondition(task)) {
            task.status = TaskStatus::WAITING_PRECONDITION;
            return;
        }
    }

    // Ready to execute - start action
    task.status = TaskStatus::EXECUTING_ACTION;
    execute_current_step(task);
}

bool TaskExecutor::check_precondition(RunningTask& task) {
    auto vertex_opt = get_current_vertex(task);
    if (!vertex_opt) {
        return false;
    }

    auto result = graph_executor_.check_step_condition(*vertex_opt, task.ctx);

    if (result == PreconditionEvaluator::Result::SATISFIED) {
        task.blocking_reason.clear();
        return true;
    }

    // Not satisfied - update blocking reason
    if (result == PreconditionEvaluator::Result::NOT_SATISFIED) {
        task.blocking_reason = "Waiting for precondition";
    } else if (result == PreconditionEvaluator::Result::NEED_SERVER) {
        task.blocking_reason = "Waiting for server query";
    }

    return false;
}

void TaskExecutor::execute_current_step(RunningTask& task) {
    auto vertex_opt = get_current_vertex(task);
    if (!vertex_opt) {
        complete_task(task, false, "Vertex not found");
        return;
    }

    const auto& vertex = *vertex_opt;

    // Create action request
    auto request_opt = graph_executor_.create_action_request(task.ctx, vertex);
    if (!request_opt) {
        // Not an action step (e.g., condition) - evaluate and move on
        if (vertex.step().step_type() == fleet::v1::STEP_TYPE_CONDITION) {
            std::string next_id = graph_executor_.evaluate_condition(
                vertex.step().condition(), task.ctx);

            if (!next_id.empty()) {
                task.ctx.current_vertex_id = next_id;
                task.ctx.current_step_index++;
                task.status = TaskStatus::RUNNING;
            } else {
                complete_task(task, false, "Condition evaluation failed");
            }
            return;
        }

        complete_task(task, false, "Not an executable step");
        return;
    }

    const auto& request = *request_opt;

    log.info("[EXEC] ▶ Executing step: {}", vertex.id());
    log.info("[EXEC]   Action: {} on {}",
             request.action_type, request.action_server);
    log.info("[EXEC]   Task: {}, Step: {}",
             request.task_id, request.step_id);

    // Log parameters (before/after variable substitution)
    if (!request.params_json.empty()) {
        log.info("[EXEC]   Params: {}", truncate(request.params_json, 100));
    }

    // Log available variables
    if (!task.ctx.variables.empty()) {
        log.debug("[EXEC]   Available variables ({}):", task.ctx.variables.size());
        for (const auto& [k, v] : task.ctx.variables) {
            log.debug("[EXEC]     ${{{}}}: {}", k, truncate(v, 40));
        }
    }

    // Get executor
    ActionExecutor* executor = get_executor(task.robot_id);
    if (!executor) {
        complete_task(task, false, "No executor for robot: " + task.robot_id);
        return;
    }

    // Start action
    task.action_pending = true;
    task.ctx.step_started_at = std::chrono::steady_clock::now();

    if (!executor->execute(request)) {
        task.action_pending = false;
        complete_task(task, false, "Failed to start action");
        return;
    }

    // Report state update (now executing)
    report_state_to_server(task);
}

// ============================================================
// Action Handling
// ============================================================

void TaskExecutor::on_action_result(const ActionResultInternal& result) {
    log.info("[EXEC] ◀ Action result: step={}, status={}, error='{}'",
             result.step_id, result.status, result.error);

    std::lock_guard<std::mutex> lock(tasks_mutex_);

    // Find task by task_id from result
    auto it = tasks_.find(result.task_id);
    if (it == tasks_.end()) {
        log.warn("[EXEC] Task {} not found for result", result.task_id);
        return;
    }

    RunningTask& task = it->second;
    task.action_pending = false;

    handle_step_result(task, result);
}

void TaskExecutor::handle_step_result(
    RunningTask& task,
    const ActionResultInternal& result) {

    // ★ Apply step result - this is the key function that enables result passing!
    graph_executor_.apply_step_result(
        task.ctx,
        result.step_id,
        action_status_to_outcome(result.status),
        result.result_json
    );

    log.info("[TASK] Step {} completed: {} -> stored in variables",
             result.step_id,
             result.status == static_cast<int>(fleet::v1::ACTION_STATUS_SUCCEEDED)
                 ? "success" : "failed");

    // Report step result to server
    report_step_result_to_server(task, result);

    // Get outcome for edge following
    std::string outcome = action_status_to_outcome(result.status);

    // Get next vertex
    std::string matched_condition;
    auto next_vertex = graph_executor_.get_next_step(
        task.ctx, task.behavior_tree, outcome, &matched_condition);

    if (next_vertex) {
        log.info("[TASK] Moving to next step: {} (matched: {})",
                 task.ctx.current_vertex_id,
                 matched_condition.empty() ? outcome : matched_condition);

        // Check if terminal
        if (graph_executor_.is_terminal(*next_vertex)) {
            bool success = (next_vertex->terminal().terminal_type() ==
                           fleet::v1::TERMINAL_TYPE_SUCCESS);
            complete_task(task, success);
        } else {
            task.status = TaskStatus::RUNNING;
            report_state_to_server(task);
        }
    } else {
        // No next step found - complete with failure
        complete_task(task, false, "No next step found for outcome: " + outcome);
    }
}

std::string TaskExecutor::action_status_to_outcome(int status) {
    switch (static_cast<fleet::v1::ActionStatus>(status)) {
        case fleet::v1::ACTION_STATUS_SUCCEEDED:
            return "success";
        case fleet::v1::ACTION_STATUS_FAILED:
            return "failed";
        case fleet::v1::ACTION_STATUS_CANCELLED:
            return "cancelled";
        case fleet::v1::ACTION_STATUS_TIMEOUT:
            return "timeout";
        case fleet::v1::ACTION_STATUS_REJECTED:
            return "rejected";
        default:
            return "failed";
    }
}

// ============================================================
// Task Completion
// ============================================================

void TaskExecutor::complete_task(
    RunningTask& task,
    bool success,
    const std::string& error) {

    task.status = success ? TaskStatus::COMPLETED : TaskStatus::FAILED;
    if (!error.empty()) {
        task.blocking_reason = error;
    }

    auto elapsed = std::chrono::steady_clock::now() - task.started_at;
    auto elapsed_ms = std::chrono::duration_cast<std::chrono::milliseconds>(elapsed).count();

    log.info("[TASK] ════════════════════════════════════════════════");
    log.info("[TASK] {} Task {} ({})",
             success ? "✓" : "✗",
             task.task_id,
             success ? "SUCCESS" : "FAILED");
    log.info("[TASK]   Duration: {}ms", elapsed_ms);
    log.info("[TASK]   Steps completed: {}", task.ctx.current_step_index);
    if (!error.empty()) {
        log.info("[TASK]   Error: {}", error);
    }
    log.info("[TASK] ════════════════════════════════════════════════");

    // Report final state to server
    report_state_to_server(task);
}

float TaskExecutor::calculate_progress(const RunningTask& task) {
    // Simple progress calculation based on step index
    int total_steps = task.behavior_tree.vertices_size();
    if (total_steps <= 1) {
        return task.status == TaskStatus::COMPLETED ? 1.0f : 0.0f;
    }
    return static_cast<float>(task.ctx.current_step_index) /
           static_cast<float>(total_steps - 1);
}

// ============================================================
// Server Communication
// ============================================================

void TaskExecutor::report_state_to_server(RunningTask& task) {
    fleet::v1::TaskStateUpdate update;
    update.set_task_id(task.task_id);
    update.set_current_step_id(task.ctx.current_vertex_id);
    update.set_state(task_status_to_proto(task.status));
    update.set_progress(calculate_progress(task));
    update.set_blocking_reason(task.blocking_reason);
    update.set_timestamp_ms(now_ms());

    // Include variables (for debugging / visibility)
    for (const auto& [key, value] : task.ctx.variables) {
        (*update.mutable_variables())[key] = value;
    }

    // Wrap in AgentMessage
    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(agent_id_);
    msg->set_timestamp_ms(now_ms());
    *msg->mutable_task_state() = update;

    // Queue with OutboundMessage
    OutboundMessage out;
    out.message = msg;
    out.created_at = std::chrono::steady_clock::now();
    out.priority = 10;
    outbound_queue_.push(std::move(out));

    task.last_state_report = std::chrono::steady_clock::now();
}

void TaskExecutor::report_all_states() {
    std::lock_guard<std::mutex> lock(tasks_mutex_);

    for (auto& [task_id, task] : tasks_) {
        // Only report active tasks
        if (task.status == TaskStatus::RUNNING ||
            task.status == TaskStatus::WAITING_PRECONDITION ||
            task.status == TaskStatus::EXECUTING_ACTION) {
            report_state_to_server(task);
        }
    }
}

void TaskExecutor::report_step_result_to_server(
    const RunningTask& task,
    const ActionResultInternal& result) {

    fleet::v1::TaskStateUpdate update;
    update.set_task_id(task.task_id);
    update.set_current_step_id(task.ctx.current_vertex_id);
    update.set_state(task_status_to_proto(task.status));
    update.set_progress(calculate_progress(task));
    update.set_timestamp_ms(now_ms());

    // Include step result info
    auto* step_result = update.mutable_step_result();
    step_result->set_step_id(result.step_id);
    step_result->set_status(static_cast<fleet::v1::ActionStatus>(result.status));
    step_result->set_result_json(result.result_json);
    step_result->set_error(result.error);
    step_result->set_duration_ms(result.completed_at_ms - result.started_at_ms);

    // Wrap in AgentMessage
    auto msg = std::make_shared<fleet::v1::AgentMessage>();
    msg->set_agent_id(agent_id_);
    msg->set_timestamp_ms(now_ms());
    *msg->mutable_task_state() = update;

    // Queue with OutboundMessage
    OutboundMessage out;
    out.message = msg;
    out.created_at = std::chrono::steady_clock::now();
    out.priority = 10;
    outbound_queue_.push(std::move(out));
}

fleet::v1::TaskState TaskExecutor::task_status_to_proto(TaskStatus status) {
    switch (status) {
        case TaskStatus::PENDING:
            return fleet::v1::TASK_STATE_PENDING;
        case TaskStatus::RUNNING:
            return fleet::v1::TASK_STATE_RUNNING;
        case TaskStatus::WAITING_PRECONDITION:
            return fleet::v1::TASK_STATE_WAITING_PRECONDITION;
        case TaskStatus::EXECUTING_ACTION:
            return fleet::v1::TASK_STATE_EXECUTING_ACTION;
        case TaskStatus::COMPLETED:
            return fleet::v1::TASK_STATE_COMPLETED;
        case TaskStatus::FAILED:
            return fleet::v1::TASK_STATE_FAILED;
        case TaskStatus::CANCELLED:
            return fleet::v1::TASK_STATE_CANCELLED;
        default:
            return fleet::v1::TASK_STATE_UNKNOWN;
    }
}

// ============================================================
// Helpers
// ============================================================

std::optional<fleet::v1::Vertex> TaskExecutor::get_current_vertex(
    const RunningTask& task) {

    return graph_executor_.get_vertex(task.behavior_tree, task.ctx.current_vertex_id);
}

ActionExecutor* TaskExecutor::get_executor(const std::string& robot_id) {
    std::lock_guard<std::mutex> lock(executors_mutex_);
    auto it = executors_.find(robot_id);
    if (it != executors_.end()) {
        return it->second.get();
    }
    return nullptr;
}

std::string TaskExecutor::truncate(const std::string& str, size_t max_len) {
    if (str.length() <= max_len) {
        return str;
    }
    return str.substr(0, max_len - 3) + "...";
}

}  // namespace executor
}  // namespace robot_agent
