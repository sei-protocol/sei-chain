#!/bin/bash
# =======================================================================
# Starport Development Environment Build System
# =======================================================================


# This process uses tools and a design pattern first developed by the pikvm team for their pi-builder and os tools.
# the biggest differences between this process and theirs are:
# * we use docker buildx so we don't need to deal with qemu directly.
# * we are not offering as many choices to users and are designing around automation.
# Later we can make this work for more devices and platforms with nearly the same technique.
# Reasonable build targets include: https://archlinuxarm.org/platforms/armv8
# For example, the Odroid-N2 is the same software-wise as our Router!

# Fail on error
set -exo pipefail

# Print each command
set -o xtrace

# EXTRACT IMAGE
# Make a temporary directory
rm -rf .tmp || true
mkdir .tmp

# UNCOMMENT and add username WHEN NOT USING GITHUB ACTIONS
# docker buildx build --tag interchainaccount --file .pi/Dockerfile --platform linux/arm64 --cache-from interchainaccount:cache --cache-to interchainaccount:cache --load --progress tty .

# save the image to result-rootfs.tar
docker save --output ./.tmp/result-rootfs.tar interchainaccount

# Extract the image using docker-extract
docker run --rm --tty --volume $(pwd)/./.tmp:/root/./.tmp --workdir /root/./.tmp/.. faddat/toolbox /tools/docker-extract --root ./.tmp/result-rootfs  ./.tmp/result-rootfs.tar

# get rid of result-rootfs.tar to save space
rm ./.tmp/result-rootfs.tar

# Set hostname while the image is just in the filesystem.
sudo bash -c "echo interchainaccount > ./.tmp/result-rootfs/etc/hostname"


# ===================================================================================
# IMAGE: Make a .img file and compress it.
# Uses Techniques from Disconnected Systems:
# https://disconnected.systems/blog/raspberry-pi-archlinuxarm-setup/
# ===================================================================================


# Unmount anything on the loop device
sudo umount /dev/loop0p2 || true
sudo umount /dev/loop0p1 || true

# Detach from the loop device
sudo losetup -d /dev/loop0 || true

# Create a folder for images
rm -rf images || true
mkdir -p images

# Make the image file
fallocate -l 4G "images/interchainaccount.img"

# loop-mount the image file so it becomes a disk
sudo losetup --find --show images/interchainaccount.img

# partition the loop-mounted disk
sudo parted --script /dev/loop0 mklabel msdos
sudo parted --script /dev/loop0 mkpart primary fat32 0% 200M
sudo parted --script /dev/loop0 mkpart primary ext4 200M 100%

# format the newly partitioned loop-mounted disk
sudo mkfs.vfat -F32 /dev/loop0p1
sudo mkfs.ext4 -F /dev/loop0p2

# Use the toolbox to copy the rootfs into the filesystem we formatted above.
# * mount the disk's /boot and / partitions
# * use rsync to copy files into the filesystem
# make a folder so we can mount the boot partition
# soon will not use toolbox

sudo mkdir -p mnt/boot mnt/rootfs
sudo mount /dev/loop0p1 mnt/boot
sudo mount /dev/loop0p2 mnt/rootfs
sudo rsync -a ./.tmp/result-rootfs/boot/* mnt/boot
sudo rsync -a ./.tmp/result-rootfs/* mnt/rootfs --exclude boot
sudo mkdir mnt/rootfs/boot
sudo umount mnt/boot mnt/rootfs || true

# Drop the loop mount
sudo losetup -d /dev/loop0
