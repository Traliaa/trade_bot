FROM node:22-bookworm-slim@sha256:de170b39439eb0e5a3444ad7d60b015614a381e10fecae2e30ec9e735122f38d AS build
WORKDIR /src
RUN npm install --global --no-audit --no-fund npm@11.11.0
COPY package.json package-lock.json ./
RUN npm ci --include=dev --no-audit --no-fund
COPY . .
ENV PUBLIC_API_BASE=https://api.trade.bot.etk3.xyz PUBLIC_ADMIN_CHAT_ID=0 PUBLIC_DEV_USER_ID=0
RUN npm run check && node --test tests/*.test.mjs && npm run build

FROM nginxinc/nginx-unprivileged:stable-alpine@sha256:f5116ed4606f3eb015dd4a8e28cb9b351476dc0ad4bc6e9f93b19cec130cdf03
COPY --from=build --chown=101:101 /src/dist /usr/share/nginx/html
COPY --from=build /src/.ci-nginx.conf /etc/nginx/nginx.conf
USER 101:101
EXPOSE 8080
ENTRYPOINT ["/usr/sbin/nginx"]
CMD ["-g", "daemon off;"]
