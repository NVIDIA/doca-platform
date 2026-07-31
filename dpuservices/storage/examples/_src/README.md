# DPF Storage Manifests

The manifests in the `manifests` directory serve as templates for creating storage deployment scenarios.
They are processed by the `update.sh` script to generate scenario-specific examples in the `scenarios` folder.

## Usage

The `update.sh` script will regenerate all manifests in the `scenarios` folder based on the templates in this directory.

**Important Notes:**
- The `update.sh` script is automatically called by the `make generate-manifests-storage` target in the `Makefile` in the root of the repository
- The script removes all existing content in the `scenarios` folder before generating new manifests
