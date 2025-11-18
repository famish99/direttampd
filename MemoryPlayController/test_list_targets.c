#define _POSIX_C_SOURCE 200809L
#include "lib_memory_play_controller.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdbool.h>

int main(int argc, char* argv[]) {
    printf("=== MemoryPlay Controller - List Targets Test ===\n\n");

    // Parse command line arguments
    bool verbose = false;
    const char* host_address = NULL;
    uint32_t interface_number = 0;

    for (int i = 1; i < argc; i++) {
        if (strcmp(argv[i], "-v") == 0 || strcmp(argv[i], "--verbose") == 0) {
            verbose = true;
        } else if (i + 1 < argc && (strcmp(argv[i], "-h") == 0 || strcmp(argv[i], "--host") == 0)) {
            host_address = argv[++i];
        } else if (i + 1 < argc && (strcmp(argv[i], "-i") == 0 || strcmp(argv[i], "--interface") == 0)) {
            interface_number = atoi(argv[++i]);
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

    printf("Library initialized successfully.\n\n");

    // If no host specified, try to find one automatically
    if (!host_address) {
        printf("No host specified, discovering MemoryPlayHost instances...\n");
        MPCHostList* host_list = NULL;
        result = mpc_list_hosts(&host_list);

        if (result != MPC_SUCCESS || host_list->count == 0) {
            fprintf(stderr, "ERROR: Failed to find any MemoryPlayHost instances\n");
            fprintf(stderr, "Please specify a host with -h <address> -i <interface>\n");
            if (result == MPC_SUCCESS) mpc_free_host_list(host_list);
            mpc_cleanup();
            return 1;
        }

        // Use the first loopback host found, or just the first host
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

        printf("Using host: %s%%%u\n\n", host_address, interface_number);

        // Note: We're using the address from the host_list which will be freed,
        // so we need to make a copy
        char* addr_copy = strdup(host_address);
        mpc_free_host_list(host_list);
        host_address = addr_copy;
    }

    // List available targets
    printf("Querying Diretta targets from %s%%%u...\n", host_address, interface_number);
    MPCTargetList* target_list = NULL;
    result = mpc_list_targets(host_address, interface_number, &target_list);

    if (result != MPC_SUCCESS) {
        fprintf(stderr, "ERROR: Failed to list targets: %s\n",
                mpc_error_string(result));
        mpc_cleanup();
        return 1;
    }

    // Display results
    printf("\nFound %zu Diretta target(s):\n\n", target_list->count);

    if (target_list->count == 0) {
        printf("No Diretta targets found.\n");
        printf("Make sure Diretta target devices are available and connected.\n");
    } else {
        for (size_t i = 0; i < target_list->count; i++) {
            MPCTargetInfo* target = &target_list->targets[i];
            printf("Target #%zu:\n", i + 1);
            printf("  Address:   %s%%%u\n", target->ip_address, target->interface_number);
            printf("  Name:      %s\n", target->target_name);
            printf("\n");
        }
    }

    // Cleanup
    printf("Cleaning up...\n");
    mpc_free_target_list(target_list);
    mpc_cleanup();

    printf("Test completed successfully.\n");
    return 0;
}
