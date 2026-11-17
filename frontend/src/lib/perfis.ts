// Perfis do sistema (iguais aos do backend, usuarios.perfil):
//   administrador — administrador do ambiente: tudo (usuários, cadastros, integrações, fila…)
//   atendente     — só Nova solicitação e Fila de atendimento
//   solicitante   — abre solicitações e acompanha (Nova solicitação e Painel)
export type Perfil = 'solicitante' | 'atendente' | 'administrador';

export const PERFIS: { valor: Perfil; rotulo: string }[] = [
  { valor: 'solicitante', rotulo: 'Solicitante' },
  { valor: 'atendente', rotulo: 'Atendente' },
  { valor: 'administrador', rotulo: 'Administrador' },
];

/** Quem opera a Fila de atendimento: atendente e administrador. */
export const podeAtender = (perfil?: Perfil | string | null): boolean =>
  perfil === 'administrador' || perfil === 'atendente';

/** Página em que cada perfil cai ao entrar: o atendente trabalha na fila; os demais veem o Painel. */
export const paginaInicial = (perfil?: Perfil | string | null): string =>
  perfil === 'atendente' ? '/fila' : '/painel';
