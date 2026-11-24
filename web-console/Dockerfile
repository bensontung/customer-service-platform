FROM node:20-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
ARG VITE_DINGTALK_CORP_ID
ARG VITE_DINGTALK_CLIENT_ID
ENV VITE_DINGTALK_CORP_ID=$VITE_DINGTALK_CORP_ID
ENV VITE_DINGTALK_CLIENT_ID=$VITE_DINGTALK_CLIENT_ID
RUN npm run build
FROM nginxinc/nginx-unprivileged:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 8080
