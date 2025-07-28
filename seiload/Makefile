# Loadtest_v2 Makefile
# Generates Go bindings for smart contracts and builds the seiload CLI

# Directories
CONTRACTS_DIR := generator/contracts
SCENARIOS_DIR := generator/scenarios
BINDINGS_DIR := generator/bindings
BUILD_DIR := build

# Binary configuration
BINARY_NAME := seiload
INSTALL_PATH := $(GOPATH)/bin
ifeq ($(GOPATH),)
	INSTALL_PATH := $(HOME)/go/bin
endif

# Tools
SOLC := solc
ABIGEN := abigen

# Find all .sol files in contracts directory
SOL_FILES := $(wildcard $(CONTRACTS_DIR)/*.sol)
CONTRACT_NAMES := $(basename $(notdir $(SOL_FILES)))

# Generated files
ABI_FILES := $(addprefix $(BUILD_DIR)/, $(addsuffix .abi, $(CONTRACT_NAMES)))
BIN_FILES := $(addprefix $(BUILD_DIR)/, $(addsuffix .bin, $(CONTRACT_NAMES)))
BINDING_FILES := $(addprefix $(BINDINGS_DIR)/, $(addsuffix .go, $(CONTRACT_NAMES)))
SCENARIO_TEMPLATE_FILES := $(addprefix $(SCENARIOS_DIR)/, $(addsuffix .go, $(CONTRACT_NAMES)))

.PHONY: generate clean help build-cli install

# Default target
help:
	@echo "Available targets:"
	@echo "  generate  - Generate Go bindings and scenario templates for all contracts"
	@echo "  clean     - Remove generated files"
	@echo "  help      - Show this help message"
	@echo "  build-cli - Build the seiload CLI"
	@echo "  install   - Install the seiload CLI"

# Main generate target
generate: $(BINDING_FILES) $(SCENARIO_TEMPLATE_FILES)
	@echo "🏭 Updating scenario factory..."
	@./scripts/update_factory.sh $(CONTRACT_NAMES)
	@echo "✅ Generated bindings and scenario templates for contracts: $(CONTRACT_NAMES)"

# Create build directory
$(BUILD_DIR):
	@mkdir -p $(BUILD_DIR)

# Create bindings directory
$(BINDINGS_DIR):
	@mkdir -p $(BINDINGS_DIR)

# Create scenarios directory
$(SCENARIOS_DIR):
	@mkdir -p $(SCENARIOS_DIR)

# Compile Solidity contracts to ABI and bytecode
$(BUILD_DIR)/%.abi $(BUILD_DIR)/%.bin: $(CONTRACTS_DIR)/%.sol | $(BUILD_DIR)
	@echo "🔨 Compiling $< ..."
	$(SOLC) --abi --bin --overwrite -o $(BUILD_DIR) $<
	@# solc outputs ContractName.abi and ContractName.bin, ensure they match our expected names
	@CONTRACT_NAME=$$(basename $< .sol); \
	TARGET_NAME=$$(basename $@ | sed 's/\.\(abi\|bin\)$$//'); \
	if [ "$${CONTRACT_NAME}" != "$${TARGET_NAME}" ]; then \
		if [ -f "$(BUILD_DIR)/$${CONTRACT_NAME}.abi" ]; then \
			mv "$(BUILD_DIR)/$${CONTRACT_NAME}.abi" "$(BUILD_DIR)/$${TARGET_NAME}.abi"; \
		fi; \
		if [ -f "$(BUILD_DIR)/$${CONTRACT_NAME}.bin" ]; then \
			mv "$(BUILD_DIR)/$${CONTRACT_NAME}.bin" "$(BUILD_DIR)/$${TARGET_NAME}.bin"; \
		fi; \
	fi

# Generate Go bindings using abigen
$(BINDINGS_DIR)/%.go: $(BUILD_DIR)/%.abi $(BUILD_DIR)/%.bin | $(BINDINGS_DIR)
	@echo "⚡ Generating Go binding for $* ..."
	$(ABIGEN) --abi $(BUILD_DIR)/$*.abi --bin $(BUILD_DIR)/$*.bin --pkg bindings --type $* --out $@

# Generate scenario template files (only if they don't exist)
$(SCENARIOS_DIR)/%.go: | $(SCENARIOS_DIR)
	@./scripts/generate_scenario_template.sh $* $@

# Clean generated files
clean:
	@echo "🧹 Cleaning generated files ..."
	@rm -rf $(BUILD_DIR) $(BINDINGS_DIR)
	@echo "✅ Cleaned up generated files"

# Check if required tools are installed
check-tools:
	@echo "🔍 Checking required tools ..."
	@which $(SOLC) > /dev/null || (echo "❌ solc not found. Install with: npm install -g solc" && exit 1)
	@which $(ABIGEN) > /dev/null || (echo "❌ abigen not found. Install with: go install github.com/ethereum/go-ethereum/cmd/abigen@latest" && exit 1)
	@echo "✅ All required tools are available"

# Install tools (optional convenience target)
install-tools:
	@echo "📦 Installing required tools ..."
	@echo "Installing solc ..."
	@npm install -g solc || echo "⚠️  Failed to install solc via npm"
	@echo "Installing abigen ..."
	@go install github.com/ethereum/go-ethereum/cmd/abigen@latest
	@echo "✅ Tools installation complete"

# Build the seiload CLI binary
build-cli: | $(BUILD_DIR)
	@echo "🔨 Building CLI"
	@go build -o $(BUILD_DIR)/$(BINARY_NAME) main.go
	@echo "✅ Built CLI: $(BUILD_DIR)/$(BINARY_NAME)"

# Install the seiload CLI
install: build-cli
	@echo "📦 Installing CLI ..."
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_PATH)/$(BINARY_NAME)
	@echo "✅ Installed CLI": $(BINARY_NAME)
