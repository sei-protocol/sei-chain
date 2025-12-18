Results.csv holdsthe results of running SDK benchmark with IAVL pruning testing with different pruning parameters.

## Schema
every - keepEvery configuration
recent - keepRecent configuration
sim_time - SDK benchmark simulation run time
max_cpu - maximum CPU during simulation
max_mem - maximum memory usage during simulation

## Steps to reproduce the results

1. SSH into the machine you want to run the simulation and clone the Cosmos SDK
`git clone http://github.com/cosmos/cosmos-sdk`

2. Checkout the tim/pruning-test branch
`git checkout tim/pruning-test`

3. Run the grid_search.py script
`python grid_search.py`

If you want to try a different range of pruning params, you can modify the script to change the min and max ranges.
