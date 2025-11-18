#define _POSIX_C_SOURCE 200809L
#include "lib_memory_play_controller.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdbool.h>

void print_usage(const char* progname) {
    printf("Usage: %s [options] <audio_file1> [audio_file2 ...]\n", progname);
    printf("\nOptions:\n");
    printf("  -h, --host <address>      Host IPv6 address (default: auto-discover)\n");
    printf("  -i, --interface <number>  Network interface number (default: 0)\n");
    printf("  -l, --loop                Enable loop playback\n");
    printf("  -v, --verbose             Enable verbose logging\n");
    printf("\nExamples:\n");
    printf("  %s track1.flac track2.flac\n", progname);
    printf("  %s -h ::1 -i 0 -l album/*.wav\n", progname);
}

int main(int argc, char* argv[]) {
    printf("=== MemoryPlay Controller - Upload Audio Test ===\n\n");

    // Parse command line arguments
    bool verbose = false;
    bool loop_mode = false;
    const char* host_address = NULL;
    uint32_t interface_number = 0;
    char** audio_files = NULL;
    int audio_file_count = 0;

    for (int i = 1; i < argc; i++) {
        if (strcmp(argv[i], "-v") == 0 || strcmp(argv[i], "--verbose") == 0) {
            verbose = true;
        } else if (strcmp(argv[i], "-l") == 0 || strcmp(argv[i], "--loop") == 0) {
            loop_mode = true;
        } else if (i + 1 < argc && (strcmp(argv[i], "-h") == 0 || strcmp(argv[i], "--host") == 0)) {
            host_address = argv[++i];
        } else if (i + 1 < argc && (strcmp(argv[i], "-i") == 0 || strcmp(argv[i], "--interface") == 0)) {
            interface_number = atoi(argv[++i]);
        } else if (strcmp(argv[i], "--help") == 0) {
            print_usage(argv[0]);
            return 0;
        } else {
            // Assume it's an audio file
            if (audio_files == NULL) {
                audio_files = &argv[i];
                audio_file_count = argc - i;
                break;
            }
        }
    }

    if (audio_file_count == 0) {
        fprintf(stderr, "ERROR: No audio files specified\n\n");
        print_usage(argv[0]);
        return 1;
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

        printf("Using host: %s%%%u\n", host_address, interface_number);

        // Make a copy of the address
        char* addr_copy = strdup(host_address);
        mpc_free_host_list(host_list);
        host_address = addr_copy;
    }

    // Open all audio files
    printf("\nOpening %d audio file(s)...\n", audio_file_count);
    MPCWavHandle* wav_handles = (MPCWavHandle*)calloc(audio_file_count, sizeof(MPCWavHandle));
    if (!wav_handles) {
        fprintf(stderr, "ERROR: Memory allocation failed\n");
        mpc_cleanup();
        return 1;
    }

    MPCFormatHandle format = NULL;
    int opened_count = 0;

    for (int i = 0; i < audio_file_count; i++) {
        printf("  [%d/%d] %s\n", i + 1, audio_file_count, audio_files[i]);
        result = mpc_wav_open(audio_files[i], &wav_handles[i]);
        if (result != MPC_SUCCESS) {
            fprintf(stderr, "ERROR: Failed to open %s: %s\n",
                    audio_files[i], mpc_error_string(result));
            goto cleanup;
        }
        opened_count++;

        // Get format from the first file
        if (i == 0) {
            result = mpc_wav_get_format(wav_handles[i], &format);
            if (result != MPC_SUCCESS) {
                fprintf(stderr, "ERROR: Failed to get format: %s\n",
                        mpc_error_string(result));
                goto cleanup;
            }
        }
    }

    printf("\nAll files opened successfully.\n");

    // Upload to host
    printf("\nUploading to %s%%%u%s...\n", host_address, interface_number,
           loop_mode ? " (loop mode)" : "");

    result = mpc_upload_audio(host_address, interface_number,
                              wav_handles, audio_file_count,
                              format, loop_mode);

    if (result != MPC_SUCCESS) {
        fprintf(stderr, "\nERROR: Upload failed: %s\n", mpc_error_string(result));
        goto cleanup;
    }

    printf("\n=== Upload completed successfully! ===\n");
    result = 0;

cleanup:
    // Close all opened WAV files
    printf("\nCleaning up...\n");
    for (int i = 0; i < opened_count; i++) {
        if (wav_handles[i]) {
            mpc_wav_close(wav_handles[i]);
        }
    }
    free(wav_handles);

    mpc_cleanup();
    return result == MPC_SUCCESS ? 0 : 1;
}
