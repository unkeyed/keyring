# The Keyring landing page: static files behind nginx on port 8080.
# Unkey builds it with root directory site/.
FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY index.html app.js /usr/share/nginx/html/
EXPOSE 8080
