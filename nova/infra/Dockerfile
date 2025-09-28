FROM python:3.10
WORKDIR /nova
COPY . .
RUN pip install --no-cache-dir requests pyyaml
CMD ["python3", "cli/control.py"]
