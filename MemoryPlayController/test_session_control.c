#define _POSIX_C_SOURCE 200809L
#define _DEFAULT_SOURCE
#include "lib_memory_play_controller.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdbool.h>
#include <unistd.h>
#include <inttypes.h>

const char* status_string(MPCPlaybackStatus status) {
    switch (status) {
        case MPC_STATUS_DISCONNECTED: return "Disconnected";
        case MPC_STATUS_PLAYING: return "Playing";
        case MPC_STATUS_PAUSED: return "Paused";
        default: return "Unknown";
    }
}

void print_usage(const char* progname) {
    printf("Usage: %s [options] [command]\n", progname);
    printf("\nCommands:\n");
    printf("  connect    - Connect to a target (default)\n");
    printf("  play       - Start playback\n");
    printf("  pause      - Pause playback\n");
    printf("  status     - Show current status\n");
    printf("  tags       - Show tag list\n");
    printf("  forward    - Seek forward 60 seconds\n");
    printf("  backward   - Seek backward 60 seconds\n");
    printf("  start      - Seek to beginning\n");
    printf("  seek       - Seek to absolute position (use -s to specify seconds)\n");
    printf("  quit       - Stop playback\n");
    printf("\nOptions:\n");
    printf("  -h, --host <address>      Host IPv6 address (default: auto-discover)\n");
    printf("  -i, --interface <number>  Network interface number (default: 0)\n");
    printf("  -n, --iterations <count>  Number of times to run status (default: 1)\n");
    printf("  -s, --seconds <position>  Seek position in seconds (for seek command)\n");
    printf("  -v, --verbose             Enable verbose logging\n");
}

int main(int argc, char* argv[]) {
    printf("=== MemoryPlay Controller - Session Control Test ===\n\n");

    // Parse command line arguments
    bool verbose = false;
    const char* host_address = NULL;
    uint32_t interface_number = 0;
    int status_iterations = 1;  // Default to 1 iteration
    int64_t seek_position = 0;   // Seek position in seconds
    const char* command = "status";  // Default command

    for (int i = 1; i < argc; i++) {
        if (strcmp(argv[i], "-v") == 0 || strcmp(argv[i], "--verbose") == 0) {
            verbose = true;
        } else if (i + 1 < argc && (strcmp(argv[i], "-h") == 0 || strcmp(argv[i], "--host") == 0)) {
            host_address = argv[++i];
        } else if (i + 1 < argc && (strcmp(argv[i], "-i") == 0 || strcmp(argv[i], "--interface") == 0)) {
            interface_number = atoi(argv[++i]);
        } else if (i + 1 < argc && (strcmp(argv[i], "-n") == 0 || strcmp(argv[i], "--iterations") == 0)) {
            status_iterations = atoi(argv[++i]);
        } else if (i + 1 < argc && (strcmp(argv[i], "-s") == 0 || strcmp(argv[i], "--seconds") == 0)) {
            seek_position = atoll(argv[++i]);
        } else if (strcmp(argv[i], "--help") == 0) {
            print_usage(argv[0]);
            return 0;
        } else {
            // Assume it's the command
            command = argv[i];
        }
    }

    // Initialize the library
    MPCConfig config = {
        .enable_logging = true,
        .verbose_mode = verbose
    };

    printf("Initializing library...\n");
    int result = mpc_init(&config);
    if (result != MPC_SUCCESS) {
        fprintf(stderr, "ERROR: Failed to initialize library: %s\n",
                mpc_error_string(result));
        return 1;
    }

    // If no host specified, try to find one automatically
    if (!host_address) {
        printf("Discovering MemoryPlayHost instances...\n");
        MPCHostList* host_list = NULL;
        result = mpc_list_hosts(&host_list);

        if (result != MPC_SUCCESS || host_list->count == 0) {
            fprintf(stderr, "ERROR: Failed to find any MemoryPlayHost instances\n");
            if (result == MPC_SUCCESS) mpc_free_host_list(host_list);
            mpc_cleanup();
            return 1;
        }

        // Use the first loopback host
        for (size_t i = 0; i < host_list->count; i++) {
            if (host_list->hosts[i].is_loopback) {
                host_address = host_list->hosts[i].ip_address;
                interface_number = host_list->hosts[i].interface_number;
                break;
            }
        }

        if (!host_address) {
            host_address = host_list->hosts[0].ip_address;
            interface_number = host_list->hosts[0].interface_number;
        }

        printf("Using host: %s%%%u\n", host_address, interface_number);

        char* addr_copy = strdup(host_address);
        mpc_free_host_list(host_list);
        host_address = addr_copy;
    }

    // Create session
    printf("Creating control session...\n");
    MPCSessionHandle session = NULL;
    result = mpc_session_create(host_address, interface_number, &session);
    if (result != MPC_SUCCESS) {
        fprintf(stderr, "ERROR: Failed to create session: %s\n",
                mpc_error_string(result));
        mpc_cleanup();
        return 1;
    }

    // Execute the requested command
    printf("\nExecuting command: %s\n", command);

    MPCPlaybackStatus status;
    int64_t current_time;
    if (strcmp(command, "connect") == 0) {
        // List available targets
        printf("Listing available targets...\n");
        MPCTargetList* targets = NULL;
        result = mpc_list_targets(host_address, interface_number, &targets);
        if (result != MPC_SUCCESS || targets->count == 0) {
            printf("No targets found.\n");
            mpc_session_close(session);
            mpc_cleanup();
            return 0;
        }

        printf("Found %zu target(s), connecting to: %s\n",
               targets->count, targets->targets[0].target_name);

        // Connect to first target
        MPCTargetInfo* target = &targets->targets[0];
        result = mpc_session_connect_target(session,
                                           target->ip_address,
                                           target->interface_number);
        if (result != MPC_SUCCESS) {
            fprintf(stderr, "ERROR: Failed to connect to target: %s\n",
                    mpc_error_string(result));
            mpc_free_target_list(targets);
            mpc_session_close(session);
            mpc_cleanup();
            return 1;
        }

        printf("Successfully connected to target\n");
        mpc_free_target_list(targets);
    } else if (strcmp(command, "play") == 0) {
        result = mpc_session_play(session);
        if (result == MPC_SUCCESS) {
            printf("Play command sent\n");
            usleep(100000); // Wait 100ms
            result = mpc_session_get_play_status(session, &status);
            if (result == MPC_SUCCESS) {
                printf("Status: %s\n", status_string(status));
                if (status == MPC_STATUS_PLAYING) {
                    result = mpc_session_get_current_time(session, &current_time);
                    if (result == MPC_SUCCESS) {
                        printf("Current Time: %ld seconds\n", current_time);
                    }
                }
            }
        }
    } else if (strcmp(command, "pause") == 0) {
        result = mpc_session_pause(session);
        if (result == MPC_SUCCESS) {
            printf("Pause command sent\n");
            usleep(100000); // Wait 100ms
            result = mpc_session_get_play_status(session, &status);
            if (result == MPC_SUCCESS) {
                printf("Status: %s\n", status_string(status));
            }
        }
    } else if (strcmp(command, "status") == 0) {
        // Get status N times based on -n argument
        for (int i = 0; i < status_iterations; i++) {
            result = mpc_session_get_current_time(session, &current_time);
            if (result == MPC_SUCCESS) {
                printf("Current Time: %ld seconds\n", current_time);
            }
        }
    } else if (strcmp(command, "tags") == 0) {
        MPCTagList* tags = NULL;
        result = mpc_session_get_tag_list(session, &tags);
        if (result == MPC_SUCCESS && tags) {
            printf("Found %zu tag(s):\n", tags->count);
            for (size_t i = 0; i < tags->count; i++) {
                printf("  Tag %zu: %s\n", i, tags->tags[i].tag);
            }
            mpc_free_tag_list(tags);
        } else {
            fprintf(stderr, "Failed to get tag list\n");
        }
    } else if (strcmp(command, "forward") == 0) {
        result = mpc_session_seek(session, 60);
        if (result == MPC_SUCCESS) {
            printf("Seek forward 60 seconds\n");
        }
    } else if (strcmp(command, "backward") == 0) {
        result = mpc_session_seek(session, -60);
        if (result == MPC_SUCCESS) {
            printf("Seek backward 60 seconds\n");
        }
    } else if (strcmp(command, "start") == 0) {
        result = mpc_session_seek_to_start(session);
        if (result == MPC_SUCCESS) {
            printf("Seek to start\n");
        }
    } else if (strcmp(command, "seek") == 0) {
        result = mpc_session_seek_absolute(session, seek_position);
        if (result == MPC_SUCCESS) {
            printf("Seek to absolute position: %ld seconds\n", (long)seek_position);
        }
    } else if (strcmp(command, "quit") == 0) {
        result = mpc_session_quit(session);
        if (result == MPC_SUCCESS) {
            printf("Quit command sent\n");
            usleep(100000); // Wait 100ms
            result = mpc_session_get_play_status(session, &status);
            if (result == MPC_SUCCESS) {
                printf("Status: %s\n", status_string(status));
            }
        }
    } else {
        fprintf(stderr, "ERROR: Unknown command: %s\n", command);
        result = MPC_ERROR_INVALID_PARAM;
    }

    if (result != MPC_SUCCESS) {
        fprintf(stderr, "Command failed: %s\n", mpc_error_string(result));
    }

    // Cleanup
    mpc_session_close(session);
    mpc_cleanup();

    printf("\n%s\n", result == MPC_SUCCESS ? "Test completed successfully!" : "Test failed!");
    return result == MPC_SUCCESS ? 0 : 1;
}
