FROM ubuntu:22.04

# Build arguments for user IDs
ARG USER_UID=1337
ARG USER_GID=1337

# Install required packages
RUN apt-get update && apt-get install -y \
    openssh-server \
    rsync \
    && rm -rf /var/lib/apt/lists/*

# Create test group and user with provided UID/GID
# Handle case where group already exists (common on macOS with gid 20 = staff)
RUN if ! getent group ${USER_GID} >/dev/null; then \
      groupadd -g ${USER_GID} testgroup; \
    else \
      echo "Group with GID ${USER_GID} already exists, using existing group"; \
    fi
RUN useradd -m -s /bin/bash -u ${USER_UID} -g ${USER_GID} testuser

# Setup SSH
RUN mkdir /var/run/sshd
RUN mkdir -p /home/testuser/.ssh

# Configure SSH daemon
RUN sed -i 's/#PasswordAuthentication yes/PasswordAuthentication no/' /etc/ssh/sshd_config
RUN sed -i 's/#PubkeyAuthentication yes/PubkeyAuthentication yes/' /etc/ssh/sshd_config

# Set proper permissions - use GID instead of group name to handle existing groups
RUN chown -R ${USER_UID}:${USER_GID} /home/testuser/.ssh
RUN chmod 700 /home/testuser/.ssh

# Create mount directories and set ownership
RUN mkdir -p /mnt/data
RUN chown ${USER_UID}:${USER_GID} /mnt/data

# Copy startup script with self-destruct mechanism
COPY start.sh /start.sh
RUN chmod +x /start.sh

EXPOSE 22
CMD ["/start.sh"]