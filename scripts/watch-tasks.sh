#!/bin/bash

while true; do date; grep '\* \[ \]' FEATURE_CHECKLIST.md|wc -l; sleep 60; done
