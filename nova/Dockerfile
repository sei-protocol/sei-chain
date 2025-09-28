FROM python:3.10
WORKDIR /app
COPY . .

# Install dependencies with no-cache option for smaller image size
RUN pip install --no-cache-dir requests pyyaml

CMD ["python3", "main.py"]
