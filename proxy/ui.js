/*
All of Snowflake's DOM manipulation and inputs.
*/

class UI {

  setStatus() {}

  setActive(connected) {
    return this.active = connected;
  }

  log() {}

  setThroughput(throughput) {
    this.throughput = throughput;
  }

}

UI.prototype.active = false;
