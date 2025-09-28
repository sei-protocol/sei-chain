FROM python:3.11-slim

WORKDIR /app
COPY .. /app
RUN pip install --no-cache-dir -e .

ENV NOVA_PROFILE=/config/profile.yaml
CMD ["nova", "auto", "--profile", "/config/profile.yaml"]
