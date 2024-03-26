#!/bin/bash

# This script is used to query oracle exchange rates and twap
seid q oracle exchange-rates -o json > contracts/oracle_exchange_rates.json
seid q oracle twaps 3600 -o json > contracts/oracle_twaps.json
