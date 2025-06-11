#!/usr/bin/env python3

import json
import requests
import sys
from datetime import datetime

def extract_redfish_metrics(cache_file_path, agent_url, agent_key):
    """Extract redfish metrics from cache.log and inject them into test agent"""
    
    print(f"Reading cache data from {cache_file_path}")
    
    # Read cache file
    try:
        with open(cache_file_path, 'r') as f:
            cache_data = json.load(f)
    except Exception as e:
        print(f"Error reading cache file: {e}")
        return False
    
    # Extract only redfish metrics
    redfish_metrics = []
    for entry in cache_data.get("entries", []):
        if entry.get("probe_name") == "redfish":
            # Convert to the format expected by our injection endpoint
            redfish_metrics.append({
                "name": entry["name"],
                "value": entry["value"],
                "unit": entry.get("unit", ""),
                "timestamp": entry["timestamp"],
                "tags": entry["tags"]
            })
    
    print(f"Found {len(redfish_metrics)} redfish metrics")
    
    if not redfish_metrics:
        print("No redfish metrics found in cache")
        return False
    
    # Show a few examples
    print("\nExample metrics:")
    for i, metric in enumerate(redfish_metrics[:5]):
        tags_str = ", ".join([f"{k}:{v}" for k, v in metric["tags"].items() if k in ["pool_name", "volume_name", "drive_name", "controller"]])
        print(f"  {i+1}. {metric['name']} - {tags_str}")
    
    # Prepare injection payload
    injection_data = {
        "metrics": redfish_metrics,
        "source": "production_cache_injection"
    }
    
    # Inject into test agent
    inject_url = f"{agent_url}/api/{agent_key}/debug/inject-real-metrics"
    
    try:
        print(f"\nInjecting metrics to {inject_url}")
        response = requests.post(inject_url, json=injection_data, timeout=30)
        
        if response.status_code == 200:
            result = response.json()
            print(f"✅ Successfully injected {result.get('metrics_count', 0)} metrics")
            
            # Show test URLs
            print(f"\n🔗 Test URLs:")
            print(f"   Dashboard: {agent_url}/web/{agent_key}/dashboard")
            print(f"   PRTG API: {agent_url}/api/{agent_key}/prtg/metrics/redfish")
            print(f"   Tag Filters: {agent_url}/api/{agent_key}/info/tags/redfish")
            
            # Show contextual filtering examples
            if "pool_name" in str(redfish_metrics):
                print(f"\n🎯 Contextual filtering examples:")
                print(f"   Pool metrics: {agent_url}/api/{agent_key}/prtg/metrics/redfish?tags=pool_name:A")
                print(f"   Volume metrics: {agent_url}/api/{agent_key}/prtg/metrics/redfish?tags=volume_name:Volume001")
                print(f"   Drive metrics: {agent_url}/api/{agent_key}/prtg/metrics/redfish?tags=drive_name:Drive%200.0")
            
            return True
        else:
            print(f"❌ Injection failed: {response.status_code} - {response.text}")
            return False
            
    except Exception as e:
        print(f"❌ Error injecting metrics: {e}")
        return False

if __name__ == "__main__":
    cache_file = "/Users/matthieu/Downloads/cache.log"
    agent_url = "http://localhost:8080"
    agent_key = "2a5d71c5-706e-43ce-8a10-8ee252f85772"
    
    success = extract_redfish_metrics(cache_file, agent_url, agent_key)
    sys.exit(0 if success else 1)