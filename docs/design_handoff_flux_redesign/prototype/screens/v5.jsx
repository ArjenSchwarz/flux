// v5 — final round
// • Off-peak block: drop title, swap Solar surplus → Lowest + 15m avg load
// • Add tab bar at top of every screen (Dashboard / Today / History)
// • In-app font picker is a handover note (deferred)

const V5 = {
  bg: '#0a0a0c',
  panel: 'rgba(255,255,255,0.04)',
  border: 'rgba(255,255,255,0.07)',
  text: '#fff', secondary: 'rgba(235,235,245,0.55)', tertiary: 'rgba(235,235,245,0.32)',
  amber: '#ffb347', offpeak: '#5ac8fa',
  grid: '#ff6b6b', gridExp: '#7be0a3', battery: '#bf5af2', soc: '#ffd089',
  load: '#f5e9d8',
  ui: '-apple-system, BlinkMacSystemFont, "SF Pro Text", system-ui, sans-serif',
  mono: 'ui-monospace, "SF Mono", Menlo, Monaco, monospace',
  hero: '"Geist", -apple-system, system-ui, sans-serif',
};

function V5Panel({ children, style, ...rest }) {
  return <div style={{
    background: V5.panel, backdropFilter: 'blur(24px) saturate(180%)',
    border: `0.5px solid ${V5.border}`, borderRadius: 18, padding: 16,
    color: V5.text, fontFamily: V5.ui, ...style,
  }} {...rest}>{children}</div>;
}

function V5Header({ sub, title, tab, onTab }) {
  return (
    <div style={{ padding: '6px 4px 14px', fontFamily: V5.ui }}>
      {/* Tab bar */}
      <V5TabBar tab={tab} onTab={onTab} />
      <div style={{ marginTop: 14 }}>
        <div style={{ fontSize: 10, fontWeight: 600, letterSpacing: 1.6, color: V5.tertiary, textTransform: 'uppercase' }}>{sub}</div>
        <div style={{ fontSize: 30, fontWeight: 600, letterSpacing: -0.6, marginTop: 2 }}>{title}</div>
      </div>
    </div>
  );
}

function V5TabBar({ tab, onTab }) {
  const tabs = ['Dashboard', 'Today', 'History'];
  return (
    <div style={{
      display: 'flex',
      background: 'rgba(255,255,255,0.05)',
      border: `0.5px solid ${V5.border}`,
      borderRadius: 10,
      padding: 3,
      gap: 2,
      backdropFilter: 'blur(20px)',
    }}>
      {tabs.map(t => (
        <button key={t} onClick={() => onTab && onTab(t)} style={{
          flex: 1, border: 'none', cursor: onTab ? 'pointer' : 'default',
          background: tab === t ? 'rgba(255,255,255,0.12)' : 'transparent',
          color: tab === t ? V5.text : V5.secondary,
          fontWeight: tab === t ? 600 : 500,
          fontSize: 12, padding: '7px 0',
          fontFamily: V5.ui, borderRadius: 8,
          letterSpacing: 0.1,
          transition: 'background 0.18s ease, color 0.18s ease',
        }}>{t}</button>
      ))}
    </div>
  );
}

function PanelHeader({ label, right }) {
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', marginBottom: 6 }}>
      <span style={{ fontSize: 10, color: V5.tertiary, textTransform: 'uppercase', letterSpacing: 1.2, fontWeight: 700, fontFamily: V5.ui }}>{label}</span>
      {right && <span style={{ fontSize: 10, color: V5.tertiary, fontFamily: V5.mono }}>{right}</span>}
    </div>
  );
}

function StatRow({ k, v, sub, accent, last }) {
  return (
    <div style={{
      display: 'flex', justifyContent: 'space-between', alignItems: 'baseline',
      padding: '7px 0', fontSize: 13,
      borderBottom: last ? 'none' : `0.5px solid ${V5.border}`,
      fontFamily: V5.ui,
    }}>
      <span style={{ color: V5.secondary }}>
        {k}{sub && <span style={{ fontSize: 9, marginLeft: 6, color: V5.tertiary, fontFamily: V5.mono }}>{sub}</span>}
      </span>
      <span style={{
        color: accent || V5.text,
        fontFamily: V5.ui, fontVariantNumeric: 'tabular-nums', fontSize: 13,
      }}>{v}</span>
    </div>
  );
}

// Off-peak: NO title row, four values: Free grid in / Battery charged / Lowest / 15m avg load
function OffPeakBlock() {
  return (
    <V5Panel style={{ marginTop: 12 }}>
      <StatRow k="Free grid in" v="3.42 kWh" accent={V5.offpeak} />
      <StatRow k="Battery charged" v="+24%" />
      <StatRow k="Lowest" v="38%" sub="SOC at 11:14" />
      <StatRow k="15m avg load" v="1.68 kW" last />
    </V5Panel>
  );
}

function SummaryBlock({ title, right }) {
  return (
    <V5Panel style={{ marginTop: 12 }}>
      <PanelHeader label={title} right={right} />
      <StatRow k="Solar produced" v="14.82 kWh" accent={V5.amber} />
      <StatRow k="House used" v="17.94 kWh" />
      <StatRow k="Grid in (peak)" v="0.84 kWh" sub="paid" accent={V5.grid} />
      <StatRow k="Grid in (off-peak)" v="3.42 kWh" sub="free" accent={V5.offpeak} />
      <StatRow k="Grid out" v="1.10 kWh" accent={V5.gridExp} />
      <StatRow k="Battery cycle" v="6.20 / 5.40 kWh" last />
    </V5Panel>
  );
}

// Charts (kept as before)
function makeReadings() {
  return Array.from({ length: 97 }, (_, i) => {
    const h = i / 4;
    const solar = Math.max(0, Math.sin((h - 6) / 12 * Math.PI)) * 3.4;
    const load  = 1.2 + Math.sin(h * 0.7) * 0.5 + (h > 17 && h < 21 ? 2.5 : 0) + (h < 6.5 ? 0.6 : 0);
    const grid  = h >= 11 && h < 14 ? 1.6 : (h > 17 && h < 21 ? 1.2 : (h < 5 ? 0.4 : 0));
    const batPwr= h < 6 ? -0.3 : (h > 6.5 && h < 11 ? 1.2 : (h >= 11 && h < 14 ? -1.6 : (h > 16 ? 1.6 : 0)));
    const soc   = Math.max(35, Math.min(95, 50 + Math.sin((h - 4) / 18) * 30 + (h > 11 && h < 14 ? 12 : 0)));
    return { h, solar, load, grid, batPwr, soc };
  });
}

function PowerChart() {
  const data = makeReadings();
  const W = 280, H = 130, pad = 16;
  const xV = h => 4 + (h / 24) * (W - 8);
  const max = 4;
  const yV = v => H - pad - (v / max) * (H - pad - 8);
  const path = (key, color) => (
    <path d={data.map((p, i) => `${i === 0 ? 'M' : 'L'} ${xV(p.h)} ${yV(p[key])}`).join(' ')}
      fill="none" stroke={color} strokeWidth="1.4" />
  );
  return (
    <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', height: H }}>
      {[1,2,3].map(v => (
        <g key={v}>
          <line x1="0" y1={yV(v)} x2={W} y2={yV(v)} stroke={V5.border} strokeWidth="0.4" />
          <text x="2" y={yV(v) - 1} fontSize="7" fill={V5.tertiary} fontFamily={V5.mono}>{v}</text>
        </g>
      ))}
      {path('solar', V5.amber)}
      {path('load', V5.load)}
      {path('grid', V5.grid)}
      {[0,6,12,18,24].map(h => (
        <text key={h} x={xV(h)} y={H - 2} fontSize="8" fill={V5.tertiary} fontFamily={V5.mono} textAnchor="middle">{String(h).padStart(2,'0')}</text>
      ))}
    </svg>
  );
}

function BatteryPowerChart() {
  const data = makeReadings();
  const W = 280, H = 110;
  const mid = (H - 14) / 2 + 4;
  const xV = h => 4 + (h / 24) * (W - 8);
  const max = 2.5;
  const path = data.map((p, i) => `${i === 0 ? 'M' : 'L'} ${xV(p.h)} ${mid - (p.batPwr / max) * (mid - 6)}`).join(' ');
  const fill = path + ` L ${xV(24)} ${mid} L ${xV(0)} ${mid} Z`;
  return (
    <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', height: H }}>
      <line x1="0" y1={mid} x2={W} y2={mid} stroke={V5.border} strokeWidth="0.5" />
      <path d={fill} fill={V5.battery} fillOpacity="0.18" />
      <path d={path} fill="none" stroke={V5.battery} strokeWidth="1.4" />
      <text x="3" y="10" fontSize="7" fill={V5.tertiary} fontFamily={V5.mono}>+ charge</text>
      <text x="3" y={H - 16} fontSize="7" fill={V5.tertiary} fontFamily={V5.mono}>− discharge</text>
      {[0,6,12,18,24].map(h => (
        <text key={h} x={xV(h)} y={H - 2} fontSize="8" fill={V5.tertiary} fontFamily={V5.mono} textAnchor="middle">{String(h).padStart(2,'0')}</text>
      ))}
    </svg>
  );
}

function SOCChart() {
  const data = makeReadings();
  const W = 280, H = 110, pad = 16;
  const xV = h => 4 + (h / 24) * (W - 8);
  const yV = v => pad + (1 - v/100) * (H - pad - 14);
  const path = data.map((p, i) => `${i === 0 ? 'M' : 'L'} ${xV(p.h)} ${yV(p.soc)}`).join(' ');
  const fill = path + ` L ${xV(24)} ${H - 14} L ${xV(0)} ${H - 14} Z`;
  return (
    <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', height: H }}>
      {[25, 50, 75].map(v => (
        <g key={v}>
          <line x1="0" y1={yV(v)} x2={W} y2={yV(v)} stroke={V5.border} strokeWidth="0.4" />
          <text x="2" y={yV(v) - 1} fontSize="7" fill={V5.tertiary} fontFamily={V5.mono}>{v}</text>
        </g>
      ))}
      <path d={fill} fill={V5.soc} fillOpacity="0.16" />
      <path d={path} fill="none" stroke={V5.soc} strokeWidth="1.5" />
      {[0,6,12,18,24].map(h => (
        <text key={h} x={xV(h)} y={H - 2} fontSize="8" fill={V5.tertiary} fontFamily={V5.mono} textAnchor="middle">{String(h).padStart(2,'0')}</text>
      ))}
    </svg>
  );
}

function Lg({ c, t }) {
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
      <span style={{ width: 8, height: 8, borderRadius: 2, background: c }} />
      {t}
    </span>
  );
}

// ──────── Dashboard
function DashboardV5({ tab, onTab }) {
  return (
    <div style={{ background: V5.bg, minHeight: '100%', padding: '0 16px 24px', color: V5.text, fontFamily: V5.ui }}>
      <V5Header sub="Now · 17:38 · May 4" title="Battery" tab={tab} onTab={onTab} />

      <V5Panel style={{ padding: 20 }}>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 6 }}>
          <span style={{ fontSize: 92, fontWeight: 300, lineHeight: 0.9, color: V5.amber, letterSpacing: -3.5, fontFamily: V5.hero }}>62</span>
          <span style={{ fontSize: 28, color: V5.tertiary, fontWeight: 300, fontFamily: V5.hero }}>%</span>
        </div>
        <div style={{ fontSize: 13, color: V5.secondary, marginTop: 6 }}>
          Discharging · 1.4 kW · empty by <span style={{ color: V5.amber, fontVariantNumeric: 'tabular-nums' }}>17:42</span>
        </div>
      </V5Panel>

      <V5Panel style={{ marginTop: 12, padding: 0 }}>
        <div style={{ display: 'flex' }}>
          {[
            { l: 'Solar', v: '0.42', u: 'kW', sub: 'producing', c: V5.amber },
            { l: 'House', v: '1.81', u: 'kW', sub: 'using', c: V5.text },
            { l: 'Grid', v: '0.05', u: 'kW', sub: 'exporting', c: V5.gridExp },
          ].map((m, i) => (
            <div key={m.l} style={{ flex: 1, padding: 14, borderLeft: i ? `0.5px solid ${V5.border}` : 'none' }}>
              <div style={{ fontSize: 10, color: V5.tertiary, textTransform: 'uppercase', letterSpacing: 1, fontWeight: 700 }}>{m.l}</div>
              <div style={{ display: 'flex', alignItems: 'baseline', gap: 2, marginTop: 4 }}>
                <span style={{ fontSize: 22, fontWeight: 500, color: m.c, fontVariantNumeric: 'tabular-nums' }}>{m.v}</span>
                <span style={{ fontSize: 10, color: V5.tertiary }}>{m.u}</span>
              </div>
              <div style={{ fontSize: 10, color: V5.secondary, marginTop: 2 }}>{m.sub}</div>
            </div>
          ))}
        </div>
      </V5Panel>

      <OffPeakBlock />
      <SummaryBlock title="Today so far" right="17:38" />
    </div>
  );
}

// ──────── Today
function TodayV5({ tab, onTab }) {
  return (
    <div style={{ background: V5.bg, minHeight: '100%', padding: '0 16px 24px', color: V5.text, fontFamily: V5.ui }}>
      <V5Header sub="Sun · May 4 · 2026" title="Today" tab={tab} onTab={onTab} />

      <V5Panel>
        <PanelHeader label="Power" right="kW" />
        <PowerChart />
        <div style={{ display: 'flex', gap: 14, fontSize: 10, color: V5.secondary, marginTop: 6 }}>
          <Lg c={V5.amber} t="Solar" />
          <Lg c={V5.load} t="House" />
          <Lg c={V5.grid} t="Grid" />
        </div>
      </V5Panel>

      <V5Panel style={{ marginTop: 12 }}>
        <PanelHeader label="Battery power" right="± kW" />
        <BatteryPowerChart />
      </V5Panel>

      <V5Panel style={{ marginTop: 12 }}>
        <PanelHeader label="Battery SOC" right="%" />
        <SOCChart />
      </V5Panel>

      <SummaryBlock title="Summary" right="May 4" />
      <OffPeakBlock />

      <V5Panel style={{ marginTop: 12 }}>
        <PanelHeader label="The day in five blocks" />
        <div style={{ display: 'flex', height: 8, gap: 2, marginBottom: 14 }}>
          {[
            { kwh: 3.1, c: '#5b6cff' }, { kwh: 2.1, c: V5.grid },
            { kwh: 5.0, c: V5.offpeak }, { kwh: 4.5, c: V5.grid },
            { kwh: 2.2, c: '#5b6cff', o: 0.4 },
          ].map((b, i) => <div key={i} style={{ flex: b.kwh, background: b.c, opacity: b.o ?? 1 }} />)}
        </div>
        {[
          ['Night',          '00–06:30', '3.1', '#5b6cff'],
          ['Morning peak',   '06:30–11', '2.1', V5.grid],
          ['Off-peak',       '11–14',    '5.0', V5.offpeak, true],
          ['Afternoon peak', '14–18:42', '4.5', V5.grid],
          ['Evening',        '18:42–24', '2.2', '#5b6cff'],
        ].map(([k, t, v, c, hi], i, arr) => (
          <div key={k} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '7px 0', borderBottom: i === arr.length - 1 ? 'none' : `0.5px solid ${V5.border}`, fontSize: 13 }}>
            <span style={{ width: 3, height: 18, background: c, borderRadius: 2 }} />
            <span style={{ flex: 1, color: hi ? V5.offpeak : V5.text, fontWeight: hi ? 600 : 400 }}>{k}</span>
            <span style={{ fontFamily: V5.mono, color: V5.tertiary, fontSize: 11 }}>{t}</span>
            <span style={{ fontVariantNumeric: 'tabular-nums', minWidth: 50, textAlign: 'right' }}>{v} kWh</span>
          </div>
        ))}
      </V5Panel>
    </div>
  );
}

// ──────── History (placeholder — leave for later, but show tab works)
function HistoryV5({ tab, onTab }) {
  return (
    <div style={{ background: V5.bg, minHeight: '100%', padding: '0 16px 24px', color: V5.text, fontFamily: V5.ui }}>
      <V5Header sub="Last 7 days" title="History" tab={tab} onTab={onTab} />
      <V5Panel style={{ padding: 24, textAlign: 'center' }}>
        <div style={{ fontSize: 11, color: V5.tertiary, textTransform: 'uppercase', letterSpacing: 1.4, fontWeight: 700, marginBottom: 8 }}>Deferred</div>
        <div style={{ fontSize: 14, color: V5.secondary, lineHeight: 1.5 }}>
          History redesign is parked for a later round.<br />Tabs work — tap Dashboard or Today to switch.
        </div>
      </V5Panel>
    </div>
  );
}

// Stateful screen with working tab switcher
function FluxV5() {
  const [tab, setTab] = React.useState('Dashboard');
  if (tab === 'Dashboard') return <DashboardV5 tab={tab} onTab={setTab} />;
  if (tab === 'Today') return <TodayV5 tab={tab} onTab={setTab} />;
  return <HistoryV5 tab={tab} onTab={setTab} />;
}

// Static previews of all three for the canvas
function DashboardV5Preview() { return <DashboardV5 tab="Dashboard" />; }
function TodayV5Preview() { return <TodayV5 tab="Today" />; }
function HistoryV5Preview() { return <HistoryV5 tab="History" />; }

Object.assign(window, {
  FluxV5, DashboardV5Preview, TodayV5Preview, HistoryV5Preview,
});
