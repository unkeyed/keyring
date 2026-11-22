# kyring.dev: Astro builds the landing page and renders docs/*.md, and its
# Node server serves everything on port 8080. Build from the repository root
# so docs/ is in the context: docker build -f site/Dockerfile .
FROM node:24-alpine AS build
WORKDIR /repo/site
COPY site/package.json site/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY site/ ./
COPY docs/ /repo/docs/
RUN npm run build

FROM node:24-alpine
WORKDIR /site
ENV NODE_ENV=production HOST=0.0.0.0 PORT=8080
COPY site/package.json site/package-lock.json ./
RUN npm ci --omit=dev --no-audit --no-fund && npm cache clean --force
COPY --from=build /repo/site/dist ./dist
COPY site/start.mjs ./
USER node
EXPOSE 8080
CMD ["node", "start.mjs"]
