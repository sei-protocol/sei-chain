#!/bin/sh

# Generate swagger files
ignite generate openapi -y

# Generate statik.go that embeds the swagger-ui files
statik -src=./docs/swagger-ui -dest=./docs -f -p swagger

# Update the generated statik.go file to use the namespace "swagger"
sed -i '' -e s/fs.Register\(data\)/fs.RegisterWithNamespace\(\"swagger\",\ data\)/g docs/swagger/statik.go
