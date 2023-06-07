package keeper

type optsFn func(*Keeper)

func (f optsFn) Apply(keeper *Keeper) {
	f(keeper)
}

func WithDependencyMappingGenerator(generator DependencyGeneratorMap) optsFn {
	return optsFn(func(k *Keeper) {
		k.MessageDependencyGeneratorMapper = generator
	})
}

func WithDependencyGeneratorMappings(generator DependencyGeneratorMap) optsFn {
	return optsFn(func(k *Keeper) {
		k.MessageDependencyGeneratorMapper = k.MessageDependencyGeneratorMapper.Merge(generator)
	})
}

func (oldGenerator DependencyGeneratorMap) Merge(newGenerator DependencyGeneratorMap) DependencyGeneratorMap {
	for messageKey, dependencyGenerator := range newGenerator {
		// overwrite default generator mappings with the new ones
		oldGenerator[messageKey] = dependencyGenerator
	}
	return oldGenerator
}
