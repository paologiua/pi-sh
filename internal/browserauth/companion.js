async function piShCompanion(base, destination) {
  if (location.origin !== "https://connect.raspberrypi.com") return;
  class CompanionError extends Error {
    constructor(code) { super(code); this.code = code; }
  }
  const needsReauthentication = page => [...(page.forms ?? [])].some(form => {
    const action = new URL(form.getAttribute('action') || '', location.href);
    return action.origin === location.origin && action.pathname === '/auth/raspberry_pi';
  });
  let stage = 'browser_operation_failed';
  const local = async (path, body) => {
    const response = await fetch(base + path, {
      method: body === undefined ? "GET" : "POST",
      credentials: "omit", cache: "no-store",
      headers: body === undefined ? {} : {"Content-Type": "application/json"},
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(125000)
    });
    if (!response.ok) throw new CompanionError('local_companion_failed');
    return response;
  };
  const request = async (url, options = {}) => {
    const target = new URL(url, location.href);
    if (target.origin !== location.origin) throw new CompanionError('invalid_connection_location');
    const response = await fetch(target.href, {...options, redirect: "error", signal: AbortSignal.timeout(120000)});
    if (response.status === 401) throw new CompanionError('reauthentication_required');
    if (response.status === 403) throw new CompanionError('access_denied');
    if (!response.ok) throw new CompanionError('connect_request_failed');
    return response;
  };
  try {
    if (needsReauthentication(document)) throw new CompanionError('reauthentication_required');
    let csrfToken = document.querySelector('meta[name="csrf-token"]')?.content ?? '';
    let shell = document.querySelector('[data-controller~="shell"]');
    if (!shell) {
      const devices = [...document.querySelectorAll('a[href]')].map(a => ({
        url: new URL(a.href, location.href), name: a.textContent.trim()
      })).filter(d => d.url.origin === location.origin && /^\/devices\/[0-9a-f-]+$/.test(d.url.pathname));
      const unique = [...new Map(devices.map(d => [d.url.pathname, d])).values()];
      const selected = destination ? unique.filter(d => d.name === destination || d.url.pathname.endsWith('/' + destination)) : unique;
      if (selected.length !== 1) throw new CompanionError('device_not_unique');
      const html = await (await request(selected[0].url.pathname + '/remote-shell-session')).text();
      const page = new DOMParser().parseFromString(html, 'text/html');
      // A login in another tab can rotate the session while Devices stays open.
      // Use the token from this newly authorized page, without retrying POSTs.
      csrfToken = page.querySelector('meta[name="csrf-token"]')?.content || csrfToken;
      shell = page.querySelector('[data-controller~="shell"]');
      if (!shell) {
        if (needsReauthentication(page)) throw new CompanionError('reauthentication_required');
        throw new CompanionError('shell_page_unavailable');
      }
    }
    const device = JSON.parse(shell.getAttribute('data-shell-device-value'));
    if (destination && destination !== device.id && destination !== device.name) throw new CompanionError('device_mismatch');
    const prefix = '/devices/' + encodeURIComponent(device.id);
    stage = 'ice_configuration_failed';
    const configuration = await (await request(prefix + '/ice-configuration', {headers: {Accept: 'application/json'}})).json();
    stage = 'browser_operation_failed';
    await local('configuration', {device: {id: device.id, name: device.name}, configuration});
    const offer = await (await local('offer')).json();
    const response = await request(prefix + '/connections', {
      method: 'POST',
      headers: {'X-CSRF-Token': csrfToken, 'Content-Type': 'application/octet-stream', Accept: 'application/json'},
      body: JSON.stringify(offer)
    });
    const locationHeader = response.headers.get('Location');
    if (!locationHeader) throw new CompanionError('invalid_connection_location');
    const poll = new URL(locationHeader, location.href);
    if (poll.origin !== location.origin || !poll.pathname.startsWith(prefix + '/')) throw new CompanionError('invalid_connection_location');
    for (let i = 0; i < 120; i++) {
      await new Promise(resolve => setTimeout(resolve, 1000));
      const result = await (await request(poll.href, {headers: {Accept: 'application/json'}})).json();
      if (result.answer) {
        await local('answer', result.answer);
        // Match the official client's periodic authorisation check. Stop when
        // the local CLI disappears; do not retain or export account tokens.
        for (;;) {
          await new Promise(resolve => setTimeout(resolve, 60000));
          await local('alive');
          const alive = await request(prefix + '.json');
          if (alive.status !== 204) throw new CompanionError('access_denied');
          await local('authorized', {});
        }
      }
    }
    throw new CompanionError('answer_timeout');
  } catch (error) {
    // Keep exceptions, HTML, locations and descriptions out of browser logs.
    const code = error instanceof CompanionError ? error.code : stage;
    try { await local('failure', {code}); } catch (_) {}
  }
}
