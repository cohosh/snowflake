/* global log, snowflake */

/*
Communication with a remote probe point for throughput tests
*/

// Represents a remote probe point used for throughput tests
class Probe {

  // When interacting with the Broker, snowflake must generate a unique session
  // ID so the Broker can keep track of each proxy's signalling channels.
  // On construction, this Broker object does not do anything until
  // |getClientOffer| is called.
  constructor(config) {
    this.requestThroughputTest = this.requestThroughputTest.bind(this);
    this._postRequest = this._postRequest.bind(this);

    this.config = config
    this.url = config.probeUrl;
    if (0 === this.url.indexOf('localhost', 0)) {
      // Ensure url has the right protocol + trailing slash.
      this.url = 'http://' + this.url;
    }
    if (0 !== this.url.indexOf('http', 0)) {
      this.url = 'https://' + this.url;
    }
    if ('/' !== this.url.substr(-1)) {
      this.url += '/';
    }
  }

  requestThroughputTest(id) {
    return new Promise((fulfill, reject) => {
      var xhr;
      xhr = new XMLHttpRequest();
      xhr.onreadystatechange = function() {
        if (xhr.DONE !== xhr.readyState) {
          return;
        }
        switch (xhr.status) {
          case Probe.CODE.OK:
            var response = JSON.parse(xhr.responseText);
            return fulfill(response.offer); // Should contain offer.
          default:
            log('Probe ERROR: Unexpected ' + xhr.status + ' - ' + xhr.statusText);
            snowflake.ui.setStatus(' failure. Please refresh.');
            return reject("Unexpected status");
        }
      };
      this._xhr = xhr; // Used by spec to fake async interaction
      var data = {"snowflake_id": id}
      return this._postRequest(xhr, 'api/snowflake-poll', JSON.stringify(data));
    });
  }

  // urlSuffix for the probe point is different depending on what action
  // is desired.
  _postRequest(xhr, urlSuffix, payload) {
    var err;
    try {
      xhr.open('POST', this.url + urlSuffix);
    } catch (error) {
      err = error;
      /*
      An exception happens here when, for example, NoScript allows the domain
      on which the proxy badge runs, but not the domain to which it's trying
      to make the HTTP xhr. The exception message is like "Component
      returned failure code: 0x805e0006 [nsIXMLHttpRequest.open]" on Firefox.
      */
      log('Probe: exception while connecting: ' + err.message);
      return;
    }
    return xhr.send(payload);
  }

}

Probe.CODE = {
  OK: 200
};
