#include "lib_memory_play_controller.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdbool.h>

int main(int argc, char* argv[]) {
    printf("=== MemoryPlay Controller - List Hosts Test ===\n\n");

    // Parse command line arguments for verbose mode
    bool verbose = false;
    for (int i = 1; i < argc; i++) {
        if (strcmp(argv[i], "-v") == 0 || strcmp(argv[i], "--verbose") == 0) {
            verbose = true;
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

    // List available hosts
    printf("Searching for MemoryPlayHost instances...\n");
    MPCHostList* host_list = NULL;
    result = mpc_list_hosts(&host_list);

    if (result != MPC_SUCCESS) {
        fprintf(stderr, "ERROR: Failed to list hosts: %s\n",
                mpc_error_string(result));
        mpc_cleanup();
        return 1;
    }

    // Display results
    printf("\nFound %zu MemoryPlayHost instance(s):\n\n", host_list->count);

    if (host_list->count == 0) {
        printf("No MemoryPlayHost instances found on the network.\n");
        printf("Make sure MemoryPlayHost is running and accessible.\n");
    } else {
        for (size_t i = 0; i < host_list->count; i++) {
            MPCHostInfo* host = &host_list->hosts[i];
            printf("Host #%zu:\n", i + 1);
            printf("  Address:   %s%%%u\n", host->ip_address, host->interface_number);
            printf("  Target:    %s\n", host->target_name);
            printf("  Output:    %s\n", host->output_name);
            printf("  Loopback:  %s\n", host->is_loopback ? "Yes" : "No");
            printf("\n");
        }
    }

    // Cleanup
    printf("Cleaning up...\n");
    mpc_free_host_list(host_list);
    mpc_cleanup();

    printf("Test completed successfully.\n");
    return 0;
}
