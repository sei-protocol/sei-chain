# Sei-Chain

Sei est la blockchain L1 la plus rapide offrant la meilleure infrastructure pour l'échange d'actifs numériques. La chaîne met l'accent sur la fiabilité, la sécurité et le débit élevé avant tout, permettant ainsi une toute nouvelle échelle de produits DeFi ultra-performants construits au-dessus. Le carnet d'ordres centralisé (CLOB) et le moteur de correspondance de Sei offrent une liquidité profonde et une correspondance de priorité en fonction du prix et du temps pour les traders et les applications. Les applications construites sur Sei bénéficient d'une infrastructure de carnet d'ordres intégrée, d'une liquidité profonde et d'un service de correspondance entièrement décentralisé. Les utilisateurs profitent de ce modèle d'échange avec la possibilité de sélectionner le prix, la taille et la direction de leurs transactions, associée à une protection contre l'extraction de la valeur des transactions (MEV).

Sei est une blockchain construite à l'aide de Cosmos SDK et Tendermint. Elle est construite à l'aide du noyau Cosmos SDK et Tendermint, et dispose d'un module intégré de carnet d'ordres centralisé (CLOB). Les applications décentralisées construisant sur Sei peuvent utiliser le CLOB, et d'autres blockchains basées sur Cosmos peuvent exploiter le CLOB de Sei en tant que hub de liquidité partagé et créer des marchés pour n'importe quel actif.

Conçue pour les développeurs et les utilisateurs, Sei sert d'infrastructure et de hub de liquidité partagée pour la prochaine génération de DeFi. Les applications peuvent facilement se brancher pour négocier sur l'infrastructure du carnet d'ordres de Sei et accéder à la liquidité mutualisée d'autres applications. Pour prioriser l'expérience des développeurs, le réseau Sei a intégré le module wasmd pour prendre en charge les contrats intelligents CosmWasm.

## Documentation

Pour la documentation la plus récente, veuillez visiter [https://www.sei.io/](https://www.sei.io/).

## Écosystème Sei

Sei Network est une blockchain L1 avec un carnet d'ordres intégré sur chaîne qui permet aux contrats intelligents un accès facile à une liquidité partagée. L'architecture de Sei permet des applications composables qui conservent la modularité.

Sei Network sert de noyau de correspondance de l'écosystème, offrant une fiabilité supérieure et une vitesse de transaction ultra-élevée aux partenaires de l'écosystème, chacun avec sa propre fonctionnalité et expérience utilisateur. N'importe qui peut créer une application DeFi qui tire parti de la liquidité de Sei et tout l'écosystème en bénéficie.

Les développeurs, les traders et les utilisateurs peuvent tous se connecter à Sei en tant que partenaires de l'écosystème bénéficiant d'une liquidité partagée et de primitives financières décentralisées.

## Testnet

### Pour commencer

#### Comment valider sur le Testnet Sei - c'est le Sei Testnet-1 (sei-testnet-1)

- Genèse publiée
- Pairs publiés

#### Configuration Matérielle
**Minimum**
- 64 Go de RAM
- 1 To NVME SSD
- 16 cœurs (CPU modernes)

**Système d'Exploitation**
- Linux (x86_64) ou Linux (amd64) Arch Linux recommandé

**Dépendances**
- Prérequis : go1.18+ requis.
    - Arch Linux : pacman -S go
    - Ubuntu : sudo snap install go --classic
- Prérequis : git.
    - Arch Linux : pacman -S git
    - Ubuntu : sudo apt-get install git
- Exigence facultative : GNU make.
    - Arch Linux : pacman -S make
    - Ubuntu : sudo apt-get install make

#### Étapes d'Installation de Seid

1. Cloner le dépôt git
    ```bash
    git clone https://github.com/sei-protocol/sei-chain
    cd sei-chain
    git checkout $VERSION
    make install
    ```

2. Générer des clés
    ```bash
    seid keys add [nom_de_clé]
    seid keys add [nom_de_clé] --recover pour régénérer des clés avec votre mnémonique
    seid keys add [nom_de_clé] --ledger pour générer des clés avec un appareil ledger
    ```

#### Instructions de Configuration du Validateur

- Installer le binaire Seid
- Initialiser le nœud : seid init <moniker> --chain-id sei-testnet-1
- Télécharger le fichier Genesis : wget https://github.com/sei-protocol/testnet/raw/main/sei-testnet-1/genesis.json -P $HOME/.sei/config/
- Modifier les minimum-gas-prices dans ${HOME}/.sei/config/app.toml : sed -i 's/minimum-gas-prices = ""/minimum-gas-prices = "0.01usei"/g' $HOME/.sei/config/app.toml
- Démarrer Seid en créant un service systemd pour exécuter le nœud en arrière-plan

Pour plus de détails et les instructions complètes, veuillez vous référer au fichier README.md d'origine du projet sur [GitHub](https://github.com/sei-protocol/sei-chain/blob/main/readme.md).

## Construisez avec Nous!

Si vous êtes intéressé à construire avec Sei Network : 
- Envoyez-nous un e-mail à team@seinetwork.io
- DM us sur Twitter [https://twitter.com/SeiNetwork](https://twitter.com/SeiNetwork)

[sei-chain/readme.md at main · sei-protocol/sei-chain](https://github.com/sei-protocol/sei-chain/blob/main/readme.md)
