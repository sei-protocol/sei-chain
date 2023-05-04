import multiprocessing
import os
import sys


def call_proc(cmd):
    """ This runs in a separate thread. """
    #subprocess.call(shlex.split(cmd))  # This will block until cmd finishes
    stream = os.popen(cmd)
    return stream.read()

def get_directories_with_go_test():
    stream = os.popen("find . -type f -name '*_test.go*' | sed -E 's|/[^/]+$||' |uniq")
    output = stream.read()
    # exclude ./ in the beginning of the filepath
    return [path[2:] for path in output.split("\n") if path]

def runtest_exists(dir):
    return os.path.exists(f"{dir}/runtest.sh")

def run():
    cpu_count = max(multiprocessing.cpu_count() - 2, 1)
    print(f"Starting tests on {cpu_count} processes")

    dirs = get_directories_with_go_test()

    root = os.path.dirname(os.path.abspath(__file__))
    pool = multiprocessing.Pool(cpu_count)
    commands = []
    results = []

    for dir in dirs:
        if runtest_exists(dir):
            command = f"cd {root}/{dir}; ./runtest.sh"
        else:
            command = f"cd {root}/{dir}; go test"
        commands.append(command)

    results = pool.map(call_proc, commands)
    success = True
    for dir, result in zip(dirs, results):
        print(dir)
        print(result)
        if "FAIL" in result:
            success = False

    if success:
        print("test success")
    else:
        print("test failed")
        sys.exit(1)


if __name__ == '__main__':
    run()
