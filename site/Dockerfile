# The Keyring site: the landing page plus docs/*.md rendered to HTML, behind
# nginx on port 8080. Build from the repository root so docs/ is in the
# context: docker build -f site/Dockerfile .
FROM node:24-alpine AS docs
WORKDIR /build
COPY site/docs-build/package.json site/docs-build/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY site/docs-build/ ./
COPY docs/ /docs/
RUN node build.mjs /docs /out/docs

FROM nginx:1.27-alpine
COPY site/nginx.conf /etc/nginx/conf.d/default.conf
COPY --chmod=755 site/analytics.sh /docker-entrypoint.d/40-analytics.sh
COPY site/index.html site/app.js site/base.css site/favicon.svg site/og.png /usr/share/nginx/html/
COPY --from=docs /out/docs /usr/share/nginx/html/docs
EXPOSE 8080
