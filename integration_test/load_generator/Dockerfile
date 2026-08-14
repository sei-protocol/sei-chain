FROM node:22-bookworm-slim AS build

WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

COPY hardhat.config.ts tsconfig.json tsconfig.build.json ./
COPY contracts ./contracts
COPY vendor ./vendor
COPY src ./src
RUN npm run build

FROM node:22-bookworm-slim AS runtime

ENV NODE_ENV=production \
    LOADGEN_COMPILED=1 \
    METRICS_HOST=0.0.0.0 \
    METRICS_PORT=9465
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts && npm cache clean --force
COPY --from=build --chown=node:node /app/dist ./dist
COPY --from=build --chown=node:node /app/artifacts ./artifacts
COPY --from=build --chown=node:node /app/vendor ./vendor

USER node
EXPOSE 9465
HEALTHCHECK --interval=15s --timeout=3s --start-period=30s --retries=3 \
    CMD node -e "fetch('http://127.0.0.1:9465/healthz').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
ENTRYPOINT ["node", "dist/runLoad.js"]
CMD ["run"]
