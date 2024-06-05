#!/bin/sh
statik -src=./docs/swagger-ui -dest=./docs -f -p swagger
sed -i '' -e s/fs.Register\(data\)/fs.RegisterWithNamespace\(\"swagger\",\ data\)/g docs/swagger/statik.go