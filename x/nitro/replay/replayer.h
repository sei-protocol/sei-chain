#include <stdarg.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>

typedef struct ByteSliceView {
  const uint8_t *ptr;
  uintptr_t len;
} ByteSliceView;

typedef struct FilePaths {
  const struct ByteSliceView *paths;
  uintptr_t count;
} FilePaths;

void replay(struct FilePaths account_file_paths,
            struct FilePaths sysvar_file_paths,
            struct FilePaths program_file_paths,
            struct FilePaths tx_file_paths,
            struct ByteSliceView output_directory);
