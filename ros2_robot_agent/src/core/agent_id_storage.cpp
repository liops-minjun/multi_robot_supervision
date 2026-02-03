// Copyright 2026 Multi-Robot Supervision System
// Agent ID storage implementation

#include "robot_agent/core/agent_id_storage.hpp"

#include <fstream>
#include <sstream>
#include <filesystem>
#include <algorithm>
#include <unistd.h>
#include <openssl/sha.h>
#include <dirent.h>
#include <net/if.h>
#include <sys/ioctl.h>
#include <cstring>
#include <iomanip>

namespace robot_agent {

AgentIdStorage::AgentIdStorage(const std::string& storage_path)
    : storage_path_(storage_path) {
}

std::optional<std::string> AgentIdStorage::load() const {
    if (!exists()) {
        return std::nullopt;
    }

    std::ifstream file(storage_path_);
    if (!file.is_open()) {
        return std::nullopt;
    }

    std::string agent_id;
    std::getline(file, agent_id);
    file.close();

    // Trim whitespace
    agent_id.erase(0, agent_id.find_first_not_of(" \t\n\r"));
    agent_id.erase(agent_id.find_last_not_of(" \t\n\r") + 1);

    // Validate: should be non-empty and look like a UUID or valid ID
    if (agent_id.empty() || agent_id.length() < 8) {
        return std::nullopt;
    }

    return agent_id;
}

bool AgentIdStorage::save(const std::string& agent_id) {
    if (agent_id.empty()) {
        return false;
    }

    if (!ensure_directory_exists()) {
        return false;
    }

    // Atomic write: write to temp file, then rename
    std::string temp_path = storage_path_ + ".tmp";

    std::ofstream file(temp_path);
    if (!file.is_open()) {
        return false;
    }

    file << agent_id << std::endl;
    file.close();

    if (file.fail()) {
        std::filesystem::remove(temp_path);
        return false;
    }

    // Atomic rename
    try {
        std::filesystem::rename(temp_path, storage_path_);
        return true;
    } catch (const std::exception&) {
        std::filesystem::remove(temp_path);
        return false;
    }
}

bool AgentIdStorage::clear() {
    if (!exists()) {
        return true;  // Already cleared
    }

    try {
        return std::filesystem::remove(storage_path_);
    } catch (const std::exception&) {
        return false;
    }
}

bool AgentIdStorage::exists() const {
    return std::filesystem::exists(storage_path_);
}

bool AgentIdStorage::ensure_directory_exists() const {
    try {
        std::filesystem::path dir = std::filesystem::path(storage_path_).parent_path();
        if (!dir.empty() && !std::filesystem::exists(dir)) {
            return std::filesystem::create_directories(dir);
        }
        return true;
    } catch (const std::exception&) {
        return false;
    }
}

std::string AgentIdStorage::generate_hardware_fingerprint() {
    std::stringstream combined;

    // 1. Read /etc/machine-id (unique per Linux installation)
    {
        std::ifstream file("/etc/machine-id");
        if (file.is_open()) {
            std::string machine_id;
            std::getline(file, machine_id);
            file.close();
            // Trim whitespace
            machine_id.erase(0, machine_id.find_first_not_of(" \t\n\r"));
            machine_id.erase(machine_id.find_last_not_of(" \t\n\r") + 1);
            combined << machine_id;
        }
    }

    // 2. Get hostname
    {
        char hostname[256] = {0};
        if (gethostname(hostname, sizeof(hostname) - 1) == 0) {
            combined << hostname;
        }
    }

    // 3. Get network interface names (sorted for consistency)
    // Only include stable physical interfaces, exclude dynamic virtual ones
    {
        std::vector<std::string> iface_names;

        DIR* d = opendir("/sys/class/net");
        if (d) {
            struct dirent* entry;
            while ((entry = readdir(d)) != nullptr) {
                std::string name = entry->d_name;
                // Skip . and ..
                if (name == "." || name == "..") continue;
                // Skip loopback
                if (name == "lo") continue;
                // Skip Docker veth interfaces (random names like veth1234abc)
                if (name.rfind("veth", 0) == 0) continue;
                // Skip Docker bridge networks (br-<random_id>)
                if (name.rfind("br-", 0) == 0) continue;
                // Skip libvirt virtual bridges
                if (name.rfind("virbr", 0) == 0) continue;
                // Skip VPN/tunnel interfaces
                if (name.rfind("tun", 0) == 0) continue;
                if (name.rfind("tap", 0) == 0) continue;

                iface_names.push_back(name);
            }
            closedir(d);
        }

        // Sort for consistency
        std::sort(iface_names.begin(), iface_names.end());
        for (const auto& name : iface_names) {
            combined << name;
        }
    }

    // Generate SHA256 hash
    std::string combined_str = combined.str();
    if (combined_str.empty()) {
        // Fallback: use a random-ish value based on PID and time
        combined_str = std::to_string(getpid()) + std::to_string(time(nullptr));
    }

    unsigned char hash[SHA256_DIGEST_LENGTH];
    SHA256(reinterpret_cast<const unsigned char*>(combined_str.c_str()),
           combined_str.length(), hash);

    // Convert first 16 bytes (32 hex chars) to string
    std::stringstream hex_stream;
    hex_stream << std::hex << std::setfill('0');
    for (int i = 0; i < 16; ++i) {
        hex_stream << std::setw(2) << static_cast<int>(hash[i]);
    }

    return hex_stream.str();
}

}  // namespace robot_agent
